package main

import (
	"fmt"
	"github.com/google/go-github/v32/github"
)

func detectSHASUM(name string) (*Version, error) {
	match := shasumRegexp.FindStringSubmatch(name)
	if len(match) < 2 {
		return nil, fmt.Errorf("nomatch %d", len(match))
	}
	result := make(map[string]string)
	for i, name := range shasumRegexp.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}
	return &Version{
		Version: result["version"],
	}, nil
}

func collectPlatforms(assets []*github.ReleaseAsset) []Platform {
	platforms := make([]Platform, 0)
	for _, a := range assets {
		match := binaryRegexp.FindStringSubmatch(*a.Name)
		if len(match) < 2 {
			continue
		}
		result := make(map[string]string)
		for i, name := range binaryRegexp.SubexpNames() {
			if i != 0 && name != "" {
				result[name] = match[i]
			}
		}
		platforms = append(platforms, Platform{
			Os:   result["os"],
			Arch: result["arch"],
		})
	}
	return platforms
}
