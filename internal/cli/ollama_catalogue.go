package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	nethtml "golang.org/x/net/html"
)

const (
	ollamaCatalogueURL      = "https://ollama.com"
	ollamaCatalogueMaxItems = 20
	ollamaCatalogueMaxHTML  = 4 << 20
)

type ollamaCatalogueModel struct {
	Name        string
	Path        string
	Description string
}

type ollamaCatalogueVariant struct {
	Tag    string
	Detail string
	SizeGB float64
}

type ollamaCatalogue interface {
	Search(context.Context, string) ([]ollamaCatalogueModel, error)
	Variants(context.Context, ollamaCatalogueModel) ([]ollamaCatalogueVariant, error)
}

type ollamaWebCatalogue struct {
	baseURL string
	client  *http.Client
}

func newOllamaWebCatalogue() ollamaWebCatalogue {
	return ollamaWebCatalogue{
		baseURL: ollamaCatalogueURL,
		client:  &http.Client{Timeout: 12 * time.Second},
	}
}

func (catalogue ollamaWebCatalogue) Search(ctx context.Context, query string) ([]ollamaCatalogueModel, error) {
	endpoint := strings.TrimRight(catalogue.baseURL, "/") + "/search?q=" + url.QueryEscape(strings.TrimSpace(query))
	document, err := catalogue.fetchHTML(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("search Ollama catalogue: %w", err)
	}
	return parseOllamaCatalogueModels(document), nil
}

func (catalogue ollamaWebCatalogue) Variants(ctx context.Context, model ollamaCatalogueModel) ([]ollamaCatalogueVariant, error) {
	endpoint := strings.TrimRight(catalogue.baseURL, "/") + model.Path
	document, err := catalogue.fetchHTML(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("load Ollama catalogue variants for %s: %w", model.Name, err)
	}
	return parseOllamaCatalogueVariants(document, model), nil
}

func (catalogue ollamaWebCatalogue) fetchHTML(ctx context.Context, endpoint string) (*nethtml.Node, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ComplyScan setup; +https://github.com/ComplyScan/ComplyScan)")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Accept-Language", "en-US,en;q=0.8")
	response, err := catalogue.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama.com returned HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, ollamaCatalogueMaxHTML+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > ollamaCatalogueMaxHTML {
		return nil, fmt.Errorf("catalogue response exceeded %d MiB", ollamaCatalogueMaxHTML>>20)
	}
	document, err := nethtml.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("parse catalogue HTML: %w", err)
	}
	return document, nil
}

func parseOllamaCatalogueModels(document *nethtml.Node) []ollamaCatalogueModel {
	models := make([]ollamaCatalogueModel, 0, ollamaCatalogueMaxItems)
	seen := make(map[string]struct{})
	walkHTML(document, func(node *nethtml.Node) bool {
		if len(models) >= ollamaCatalogueMaxItems || node.Type != nethtml.ElementNode || node.Data != "a" || !hasHTMLClasses(node, "group", "w-full") {
			return true
		}
		href := htmlAttribute(node, "href")
		name, path, valid := ollamaCatalogueModelPath(href)
		if !valid {
			return true
		}
		key := strings.ToLower(name)
		if _, duplicate := seen[key]; duplicate {
			return true
		}
		description := compactHTMLText(firstHTMLDescendant(node, func(candidate *nethtml.Node) bool {
			return candidate.Type == nethtml.ElementNode && candidate.Data == "p" && hasHTMLClasses(candidate, "max-w-lg")
		}))
		models = append(models, ollamaCatalogueModel{Name: name, Path: path, Description: description})
		seen[key] = struct{}{}
		return true
	})
	return models
}

func parseOllamaCatalogueVariants(document *nethtml.Node, model ollamaCatalogueModel) []ollamaCatalogueVariant {
	variants := make([]ollamaCatalogueVariant, 0, ollamaCatalogueMaxItems)
	seen := make(map[string]struct{})
	familyPath := strings.TrimPrefix(model.Path, "/") + ":"
	walkHTML(document, func(node *nethtml.Node) bool {
		if len(variants) >= ollamaCatalogueMaxItems || node.Type != nethtml.ElementNode || node.Data != "a" || !hasHTMLClasses(node, "sm:hidden") {
			return true
		}
		href := strings.TrimPrefix(htmlAttribute(node, "href"), "/")
		if !strings.HasPrefix(strings.ToLower(href), strings.ToLower(familyPath)) {
			return true
		}
		tag := strings.TrimPrefix(href, "library/")
		if !validOllamaCatalogueName(tag) || isOllamaCloudTag(tag) {
			return true
		}
		key := strings.ToLower(tag)
		if _, duplicate := seen[key]; duplicate {
			return true
		}
		detail := compactHTMLText(firstHTMLDescendant(node, func(candidate *nethtml.Node) bool {
			return candidate.Type == nethtml.ElementNode && candidate.Data == "p" && hasHTMLClasses(candidate, "text-neutral-500")
		}))
		variants = append(variants, ollamaCatalogueVariant{Tag: tag, Detail: detail, SizeGB: catalogueSizeGB(detail)})
		seen[key] = struct{}{}
		return true
	})
	return variants
}

func ollamaCatalogueModelPath(href string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil || parsed.IsAbs() || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	path := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	name := strings.TrimPrefix(path, "library/")
	if name == "" || strings.Contains(name, ":") || !validOllamaCatalogueName(name) {
		return "", "", false
	}
	return name, "/" + path, true
}

func validOllamaCatalogueName(value string) bool {
	if value == "" || strings.HasPrefix(value, ".") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._/-:", character) {
			continue
		}
		return false
	}
	return true
}

func isOllamaCloudTag(tag string) bool {
	separator := strings.LastIndex(tag, ":")
	return separator >= 0 && strings.Contains(strings.ToLower(tag[separator+1:]), "cloud")
}

func catalogueSizeGB(detail string) float64 {
	first, _, _ := strings.Cut(strings.TrimSpace(detail), "·")
	value := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(first), " ", ""))
	for _, unit := range []struct {
		suffix string
		factor float64
	}{
		{suffix: "TB", factor: 1000},
		{suffix: "GB", factor: 1},
		{suffix: "MB", factor: 0.001},
	} {
		if !strings.HasSuffix(value, unit.suffix) {
			continue
		}
		number, err := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
		if err == nil {
			return number * unit.factor
		}
	}
	return 0
}

func walkHTML(node *nethtml.Node, visit func(*nethtml.Node) bool) {
	if node == nil || !visit(node) {
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func firstHTMLDescendant(node *nethtml.Node, match func(*nethtml.Node) bool) *nethtml.Node {
	var found *nethtml.Node
	for child := node.FirstChild; child != nil && found == nil; child = child.NextSibling {
		if match(child) {
			return child
		}
		found = firstHTMLDescendant(child, match)
	}
	return found
}

func htmlAttribute(node *nethtml.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func hasHTMLClasses(node *nethtml.Node, required ...string) bool {
	classes := strings.Fields(htmlAttribute(node, "class"))
	for _, wanted := range required {
		found := false
		for _, class := range classes {
			if class == wanted {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func compactHTMLText(node *nethtml.Node) string {
	if node == nil {
		return ""
	}
	parts := make([]string, 0)
	walkHTML(node, func(candidate *nethtml.Node) bool {
		if candidate.Type == nethtml.TextNode {
			parts = append(parts, candidate.Data)
		}
		return true
	})
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}
