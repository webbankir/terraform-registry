/*
Copyright © 2020 Andy Lo-A-Foe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/miguelmota/go-filecache"
	"golang.org/x/crypto/openpgp/packet"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp/armor"

	"golang.org/x/crypto/openpgp"

	"golang.org/x/oauth2"

	"github.com/google/go-github/v32/github"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var (
	shasumRegexp = regexp.MustCompile(`^(?P<provider>[^_]+)_(?P<version>[^_]+)_SHA256SUMS`)
	binaryRegexp = regexp.MustCompile(`^(?P<provider>[^_]+)_(?P<version>[^_]+)_(?P<os>\w+)_(?P<arch>\w+)`)
	actionRegexp = regexp.MustCompile(`^(?P<version>[^/]+)/(?P<action>[^/]+)/(?P<os>[^/]+)/(?P<arch>\w+)`)
	storageDir   = "/opt/terraform-provider-proxy"
)

func main() {
	e := echo.New()
	e.Use(middleware.Logger())

	client := newClient()

	e.GET("/.well-known/terraform.json", serviceDiscoveryHandler())
	//e.GET("/v1/providers/hashicorp/:type/versions", client.providerHandlerHashicorp())
	e.GET("/v1/providers/:namespace/:type/*", client.providerHandler())
	e.GET("/storage/:file", fromStorage())
	_ = e.Start(":8181")

}

func fromStorage() echo.HandlerFunc {
	return func(c echo.Context) error {
		file := c.Param("file")

		data, err := os.ReadFile(storageDir + "/" + file)
		if err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "File not found")
		}

		return c.Blob(http.StatusOK, "application/octet-stream", data)
	}
}

func serviceDiscoveryHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		response := struct {
			Providers string `json:"providers.v1"`
		}{
			Providers: "/v1/providers/",
		}
		return c.JSON(http.StatusOK, response)
	}
}

func newClient() *Client {
	client := &Client{}

	if token, ok := os.LookupEnv("GITHUB_TOKEN"); ok {
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		httpClient := oauth2.NewClient(ctx, ts)

		client.github = github.NewClient(httpClient)
		client.http = httpClient
		client.authenticated = true

	} else {
		client.github = github.NewClient(nil)
	}

	return client
}

func (client *Client) getURL(c echo.Context, asset *github.ReleaseAsset) (string, error) {
	if client.authenticated {
		namespace := c.Get("namespace").(string)
		provider := c.Get("provider").(string)

		_, url, err := client.github.Repositories.DownloadReleaseAsset(context.Background(),
			namespace, provider, *asset.ID, nil)
		if err != nil {
			return "", err
		}

		return url, nil
	}

	return *asset.BrowserDownloadURL, nil
}

func getShasum(asset string, shasumURL string) (string, error) {
	resp, err := http.Get(shasumURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("not found")
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "  ")
		if len(parts) != 2 {
			continue
		}
		if parts[1] == asset {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("not found")
}

func (client *Client) providerHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		namespace := c.Param("namespace")
		typeParam := c.Param("type")
		param := c.Param("*")
		provider := "terraform-provider-" + typeParam

		versions, repos, err := client.getVersions(namespace, provider, typeParam, c.Request().URL.Path)

		if err != nil {
			return c.JSON(http.StatusBadRequest, &ErrorResponse{
				Status:  http.StatusBadRequest,
				Message: err.Error(),
			})
		}

		switch param {
		case "versions":
			response := &VersionResponse{
				ID:       namespace + "/" + typeParam,
				Versions: versions,
			}
			return c.JSON(http.StatusOK, response)
		default:
			c.Set("namespace", namespace)
			c.Set("provider", provider)
			c.Set("provider_name", typeParam)
			return client.performAction(c, param, repos)
		}
	}
}

func (client *Client) performAction(c echo.Context, param string, repos []*github.RepositoryRelease) error {
	match := actionRegexp.FindStringSubmatch(param)
	if len(match) < 2 {
		fmt.Printf("repos: %v\n", repos)
		return c.JSON(http.StatusBadRequest, &ErrorResponse{
			Status:  http.StatusBadRequest,
			Message: "invalid request",
		})
	}
	result := make(map[string]string)
	for i, name := range actionRegexp.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}
	namespace := c.Get("namespace").(string)
	provider := c.Get("provider").(string)
	version := result["version"]
	os := result["os"]
	arch := result["arch"]
	filename := fmt.Sprintf("%s_%s_%s_%s.zip", provider, version, os, arch)
	shasumFilename := fmt.Sprintf("%s_%s_SHA256SUMS", provider, version)
	shasumSigFilename := fmt.Sprintf("%s_%s_SHA256SUMS.sig", provider, version)

	downloadURL := ""
	shasumURL := ""
	shasumSigURL := ""

	cacheKey := namespace + "-" + provider + "-" + version + "-" + os + "-" + arch

	downloadResponse := DownloadResponse{}

	founded, _ := filecache.Get(cacheKey, &downloadResponse)

	if !founded {
		if namespace == "hashicorp" {
			data := DownloadResponse{}
			d, _ := http.Get("https://registry.terraform.io" + c.Request().URL.Path)
			_ = json.NewDecoder(d.Body).Decode(&data)

			downloadURL = data.DownloadURL
			shasumURL = data.ShasumsURL
			shasumSigURL = data.ShasumsSignatureURL
		} else {
			var repo *github.RepositoryRelease
			for _, r := range repos {
				for _, a := range r.Assets {
					if v, err := detectSHASUM(*a.Name); err == nil && version == v.Version {
						repo = r
						break
					}
				}
			}
			if repo == nil {
				return c.JSON(http.StatusBadRequest, &ErrorResponse{
					Status:  http.StatusBadRequest,
					Message: fmt.Sprintf("cannot find version: %s", version),
				})
			}
			for _, a := range repo.Assets {
				if *a.Name == filename {
					downloadURL, _ = client.getURL(c, a)
					continue
				}
				if *a.Name == shasumFilename {
					shasumURL, _ = client.getURL(c, a)
					continue
				}
				if *a.Name == shasumSigFilename {
					shasumSigURL, _ = client.getURL(c, a)
					continue
				}
			}
		}

		pgpPublicKey, pgpPublicKeyID, err := getPublicKey(c.Get("namespace").(string), c.Get("provider_name").(string), c.Request().URL.Path)

		if err != nil {
			return c.JSON(http.StatusBadRequest, &ErrorResponse{
				Status:  http.StatusBadRequest,
				Message: fmt.Sprintf("failed getting pgp keys %v", err),
			})
		}

		downloadURL, err = filesToStorage(downloadURL)
		if err != nil {
			return c.JSON(http.StatusBadRequest, &ErrorResponse{
				Status:  http.StatusBadRequest,
				Message: fmt.Sprintf("[WB] Error: %v", err),
			})
		}

		shasumSigURL, err = filesToStorage(shasumSigURL)
		if err != nil {
			return c.JSON(http.StatusBadRequest, &ErrorResponse{
				Status:  http.StatusBadRequest,
				Message: fmt.Sprintf("[WB] Error: %v", err),
			})
		}

		shasum, err := getShasum(filename, shasumURL)
		if err != nil {
			return c.JSON(http.StatusBadRequest, &ErrorResponse{
				Status:  http.StatusBadRequest,
				Message: fmt.Sprintf("failed getting shasum %v", err),
			})
		}

		shasumURL, err = filesToStorage(shasumURL)
		if err != nil {
			return c.JSON(http.StatusBadRequest, &ErrorResponse{
				Status:  http.StatusBadRequest,
				Message: fmt.Sprintf("[WB] Error: %v", err),
			})
		}

		downloadResponse = DownloadResponse{
			Os:                  result["os"],
			Arch:                result["arch"],
			Filename:            filename,
			DownloadURL:         downloadURL,
			ShasumsSignatureURL: shasumSigURL,
			ShasumsURL:          shasumURL,
			Shasum:              shasum,
			SigningKeys: SigningKeys{
				GpgPublicKeys: []GPGPublicKey{
					{
						KeyID:      pgpPublicKeyID,
						ASCIIArmor: pgpPublicKey,
					},
				},
			},
		}

		filecache.Set(cacheKey, downloadResponse, 24*30*time.Hour)

	} else {
		fmt.Printf("\n[2]Found key %v in cache\n", cacheKey)
	}

	switch result["action"] {
	case "download":
		return c.JSON(http.StatusOK, &downloadResponse)
	default:
		return c.JSON(http.StatusNotFound, &ErrorResponse{
			Status:  http.StatusNotFound,
			Message: fmt.Sprintf("unsupported action %s", result["action"]),
		})
	}
}

func getPublicKey(namespace string, provider string, path string) (string, string, error) {

	dir := storageDir + "/gpg/" + namespace + "/" + provider
	file := dir + "/ascii_armor"

	if _, err := os.Stat(file); os.IsNotExist(err) {

		response, err := http.Get("https://registry.terraform.io" + path)

		if err != nil {
			return "", "", err
		}
		defer response.Body.Close()

		data := DownloadResponse{}
		err = json.NewDecoder(response.Body).Decode(&data)

		if err != nil {
			return "", "", err
		}

		if response.StatusCode != 200 {
			return "", "", fmt.Errorf("not found")
		}

		if len(data.SigningKeys.GpgPublicKeys) == 0 {
			return "", "", fmt.Errorf("No GpgPublicKeys in response.")
		}

		err = os.MkdirAll(dir, 0777)

		if err != nil {
			return "", "", err
		}

		err = os.WriteFile(file, []byte(fmt.Sprintf("%v", data.SigningKeys.GpgPublicKeys[0].ASCIIArmor)), 0666)
		if err != nil {
			return "", "", err
		}
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return "", "", err
	}
	// PGP
	armored := bytes.NewReader(data)
	block, err := armor.Decode(armored)
	if err != nil {
		return "", "", err
	}
	if block == nil || block.Type != openpgp.PublicKeyType {
		return "", "", fmt.Errorf("not a public key")
	}
	reader := packet.NewReader(block.Body)
	pkt, err := reader.Next()
	if err != nil {
		return "", "", err
	}
	key, _ := pkt.(*packet.PublicKey)

	return string(data), key.KeyIdString(), nil
}
