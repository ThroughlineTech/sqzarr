package plex

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// Client notifies Plex of library changes after a successful transcode.
type Client struct {
	baseURL string
	token   string
	log     *slog.Logger
	http    *http.Client
}

// New creates a Plex client. Returns nil if baseURL or token is empty.
func New(baseURL, token string, log *slog.Logger) *Client {
	if baseURL == "" || token == "" {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		log:     log,
		http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

// NotifyFileReplaced triggers a Plex library refresh for the section
// containing replacedPath. Non-fatal: logs and returns on any error.
func (c *Client) NotifyFileReplaced(replacedPath string) {
	if c == nil {
		return
	}

	sections, err := c.listSections()
	if err != nil {
		c.log.Warn("plex: list sections failed", "error", err)
		return
	}

	sectionID := c.findSection(sections, replacedPath)
	if sectionID == "" {
		c.log.Warn("plex: no matching section for file", "path", replacedPath)
		return
	}

	if err := c.refreshSection(sectionID); err != nil {
		c.log.Warn("plex: refresh failed", "section_id", sectionID, "error", err)
		return
	}

	c.log.Info("plex: library refresh triggered", "section_id", sectionID, "path", replacedPath)
}

type plexSection struct {
	Key      string `xml:"key,attr"`
	Title    string `xml:"title,attr"`
	Location []struct {
		Path string `xml:"path,attr"`
	} `xml:"Location"`
}

type plexSections struct {
	Sections []plexSection `xml:"Directory"`
}

func (c *Client) listSections() ([]plexSection, error) {
	return c.listSectionsContext(context.Background())
}

// Verify checks library access without triggering a refresh or changing media.
func (c *Client) Verify(ctx context.Context) (int, error) {
	if c == nil {
		return 0, errors.New("Save a Plex URL and token before verifying")
	}
	sections, err := c.listSectionsContext(ctx)
	return len(sections), err
}

func (c *Client) request(ctx context.Context, path string) (*http.Response, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("Plex URL must be HTTP or HTTPS without credentials, query parameters, or fragments")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, errors.New("Invalid Plex URL")
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "application/xml")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("Could not reach Plex; check the URL, network, and TLS certificate (10-second timeout)")
	}
	return resp, nil
}

func (c *Client) listSectionsContext(ctx context.Context) ([]plexSection, error) {
	resp, err := c.request(ctx, "/library/sections")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("Plex rejected access; check the token and its library permissions")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Plex returned HTTP %d; check the server URL (redirects are not followed)", resp.StatusCode)
	}

	var container struct {
		XMLName     xml.Name      `xml:"MediaContainer"`
		Directories []plexSection `xml:"Directory"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&container); err != nil {
		return nil, errors.New("Plex returned an invalid library response; check the server URL")
	}
	return container.Directories, nil
}

func (c *Client) findSection(sections []plexSection, filePath string) string {
	cleanFile := filepath.Clean(filePath)
	for _, s := range sections {
		for _, loc := range s.Location {
			if strings.HasPrefix(cleanFile, filepath.Clean(loc.Path)) {
				return s.Key
			}
		}
	}
	return ""
}

func (c *Client) refreshSection(sectionID string) error {
	resp, err := c.request(context.Background(), "/library/sections/"+url.PathEscape(sectionID)+"/refresh")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
