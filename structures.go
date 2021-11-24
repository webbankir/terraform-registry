package main

import (
	"github.com/google/go-github/v32/github"
	"net/http"
)

type Client struct {
	github        *github.Client
	authenticated bool
	http          *http.Client
}

type Platform struct {
	Os   string `json:"os"`
	Arch string `json:"arch"`
}

type VersionResponse struct {
	ID       string      `json:"id"`
	Versions []Version   `json:"versions"`
	Warnings interface{} `json:"warnings"`
}

type GPGPublicKey struct {
	KeyID          string      `json:"key_id"`
	ASCIIArmor     string      `json:"ascii_armor"`
	TrustSignature string      `json:"trust_signature"`
	Source         string      `json:"source"`
	SourceURL      interface{} `json:"source_url"`
}

type SigningKeys struct {
	GpgPublicKeys []GPGPublicKey `json:"gpg_public_keys,omitempty"`
}

type DownloadResponse struct {
	Protocols           []string    `json:"protocols,omitempty"`
	Os                  string      `json:"os"`
	Arch                string      `json:"arch"`
	Filename            string      `json:"filename"`
	DownloadURL         string      `json:"download_url"`
	ShasumsURL          string      `json:"shasums_url"`
	ShasumsSignatureURL string      `json:"shasums_signature_url"`
	Shasum              string      `json:"shasum"`
	SigningKeys         SigningKeys `json:"signing_keys"`
}

type ErrorResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

type Version struct {
	Version      string               `json:"version"`
	Protocols    []string             `json:"protocols,omitempty"`
	Platforms    []Platform           `json:"platforms"`
	ReleaseAsset *github.ReleaseAsset `json:"-"`
}
