package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func filesToStorage(assetURL string) (string, error) {
	u, err := url.Parse(assetURL)

	if err != nil {
		return "", err
	}

	if _, err := os.Stat(storageDir + u.Path); os.IsNotExist(err) {
		items := strings.Split(u.Path, "/")
		items = items[:len(items)-1]
		newDir := fmt.Sprintf("%v%v", storageDir, strings.Join(items, "/"))
		err = os.MkdirAll(newDir, 0777)

		if err != nil {
			return "", err
		}

		resp, err := http.Get(assetURL)

		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return "", fmt.Errorf("not found")
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		//_, err = ff.Write(b)
		err = os.WriteFile(storageDir+u.Path, b, 0644)
		if err != nil {
			return "", err
		}

	}
	return "https://tf-providers.infra.tec1group.com/storage" + u.Path, nil
}
