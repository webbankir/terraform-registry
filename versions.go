package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/go-github/v32/github"
	"github.com/miguelmota/go-filecache"
	"net/http"
	"time"
)

func (client *Client) getVersions(namespace string, provider string, typeParam string, urlPath string) ([]Version, []*github.RepositoryRelease, error) {
	versions := make([]Version, 0)
	repos := make([]*github.RepositoryRelease, 0)
	cacheKey := namespace + "/" + typeParam

	founded, _ := filecache.Get(cacheKey, &versions)
	if !founded {

		if namespace == "hashicorp" {
			d, err := http.Get("https://registry.terraform.io" + urlPath)

			if err != nil {
				return versions, repos, err
			}

			data := VersionResponse{}
			err = json.NewDecoder(d.Body).Decode(&data)

			return data.Versions, repos, err
		} else {
			repos, _, err := client.github.Repositories.ListReleases(context.Background(), namespace, provider, nil)

			if err != nil {
				return versions, repos, err
			}

			versions, err = parseVersions(repos)

			if err != nil {
				return versions, repos, err
			}
		}

		_ = filecache.Set(namespace+"/"+typeParam, versions, 1*time.Hour)
	} else {
		fmt.Printf("\nFound key %v in cache\n", cacheKey)
	}
	return versions, repos, nil
}

func parseVersions(repos []*github.RepositoryRelease) ([]Version, error) {
	details := make([]Version, 0)
	for _, r := range repos {
		for _, a := range r.Assets {
			assetDetails, err := detectSHASUM(*a.Name)
			if err == nil {
				assetDetails.Platforms = collectPlatforms(r.Assets)
				details = append(details, *assetDetails)
				break
			}
		}
	}
	return details, nil
}
