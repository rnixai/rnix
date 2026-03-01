package skillpkg

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

const (
	// DefaultRegistryURL is the default community skill registry URL.
	DefaultRegistryURL = "https://registry.crux.dev"
	// defaultTimeout is the default HTTP client timeout.
	defaultTimeout = 30 * time.Second
	// maxMetadataSize limits metadata responses (index.yaml, latest.yaml) to 1 MB.
	maxMetadataSize = 1 << 20
	// maxPackageSize limits package downloads to 50 MB.
	maxPackageSize = 50 << 20
)

// HTTPClient is an interface for HTTP operations, allowing test injection.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// RegistryClient interacts with the community skill registry.
type RegistryClient struct {
	baseURL string
	http    HTTPClient
}

// NewRegistryClient creates a new RegistryClient pointing to the given registry URL.
func NewRegistryClient(baseURL string, client HTTPClient) *RegistryClient {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	// Trim trailing slash for consistent URL construction.
	baseURL = strings.TrimRight(baseURL, "/")
	return &RegistryClient{baseURL: baseURL, http: client}
}

// FetchIndex retrieves the skill index from the registry.
func (c *RegistryClient) FetchIndex() (*SkillIndex, error) {
	url := c.baseURL + "/index.yaml"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch index: %w (check network connection and retry)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch index: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataSize))
	if err != nil {
		return nil, fmt.Errorf("read index response: %w", err)
	}

	var index SkillIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return &index, nil
}

// Resolve resolves the latest version metadata for a named skill.
func (c *RegistryClient) Resolve(name string) (*SkillVersion, error) {
	url := fmt.Sprintf("%s/packages/%s/latest.yaml", c.baseURL, name)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w (check network connection and retry)", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("skill %q not found in registry", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resolve %s: HTTP %d", name, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataSize))
	if err != nil {
		return nil, fmt.Errorf("read resolve response: %w", err)
	}

	var ver SkillVersion
	if err := yaml.Unmarshal(data, &ver); err != nil {
		return nil, fmt.Errorf("parse version metadata for %s: %w", name, err)
	}
	return &ver, nil
}

// Fetch downloads a skill package from the registry.
func (c *RegistryClient) Fetch(name string, ver *SkillVersion) (*SkillPackage, error) {
	url := ver.URL
	if url == "" {
		url = fmt.Sprintf("%s/packages/%s/%s.tar.gz", c.baseURL, name, ver.Version)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w (check network connection and retry)", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("package %s@%s not found in registry", name, ver.Version)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", name, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPackageSize))
	if err != nil {
		return nil, fmt.Errorf("read package data for %s: %w", name, err)
	}

	return &SkillPackage{
		Name:     name,
		Version:  ver.Version,
		Checksum: ver.Checksum,
		Data:     data,
	}, nil
}

// Verify checks the SHA256 integrity of a downloaded skill package.
func (c *RegistryClient) Verify(pkg *SkillPackage) error {
	if pkg.Checksum == "" {
		return fmt.Errorf("package %s has no checksum to verify", pkg.Name)
	}

	expected := strings.TrimPrefix(pkg.Checksum, "sha256:")
	h := sha256.Sum256(pkg.Data)
	actual := fmt.Sprintf("%x", h)

	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: expected sha256:%s, got sha256:%s", pkg.Name, expected, actual)
	}
	return nil
}
