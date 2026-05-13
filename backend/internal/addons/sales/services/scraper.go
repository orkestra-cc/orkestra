package services

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"

	"github.com/orkestra/backend/internal/addons/sales/models"
	"github.com/orkestra/backend/internal/shared/config"
)

// Scraper extracts structured company data from a website
type Scraper struct {
	timeout  time.Duration
	maxDepth int
	logger   *slog.Logger
}

// NewScraper creates a new web scraper
func NewScraper(cfg config.SalesConfig, logger *slog.Logger) *Scraper {
	return &Scraper{
		timeout:  cfg.ScraperTimeout,
		maxDepth: cfg.ScraperMaxDepth,
		logger:   logger.With(slog.String("component", "scraper")),
	}
}

// ScrapeCompany extracts structured data from a company website
func (s *Scraper) ScrapeCompany(targetURL string) (*models.ScrapedCompanyData, error) {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
		targetURL = parsed.String()
	}

	data := &models.ScrapedCompanyData{
		URL: targetURL,
	}

	var allText strings.Builder
	visitedPages := 0

	c := colly.NewCollector(
		colly.AllowedDomains(parsed.Hostname(), "www."+parsed.Hostname()),
		colly.MaxDepth(s.maxDepth),
	)
	c.SetRequestTimeout(s.timeout)

	// Limit concurrency and add polite delay
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: 2,
		Delay:       500 * time.Millisecond,
	})

	// Extract page title
	c.OnHTML("title", func(e *colly.HTMLElement) {
		title := strings.TrimSpace(e.Text)
		if title != "" {
			data.PageTitles = append(data.PageTitles, title)
		}
	})

	// Extract meta description and company-related meta tags
	c.OnHTML("meta", func(e *colly.HTMLElement) {
		name := strings.ToLower(e.Attr("name"))
		property := strings.ToLower(e.Attr("property"))
		content := strings.TrimSpace(e.Attr("content"))
		if content == "" {
			return
		}

		switch {
		case name == "description" || property == "og:description":
			if data.Description == "" {
				data.Description = content
			}
		case property == "og:site_name":
			if data.CompanyName == "" {
				data.CompanyName = content
			}
		}
	})

	// Extract heading text for company name detection
	c.OnHTML("h1", func(e *colly.HTMLElement) {
		text := strings.TrimSpace(e.Text)
		if text != "" && data.CompanyName == "" && len(text) < 100 {
			data.CompanyName = text
		}
	})

	// Extract social links
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		lowerHref := strings.ToLower(href)
		for _, social := range []string{"linkedin.com", "twitter.com", "x.com", "facebook.com", "instagram.com", "github.com"} {
			if strings.Contains(lowerHref, social) {
				data.SocialLinks = appendUnique(data.SocialLinks, href)
				break
			}
		}
	})

	// Extract about/team page text
	c.OnHTML("body", func(e *colly.HTMLElement) {
		pageURL := e.Request.URL.String()
		lowerURL := strings.ToLower(pageURL)

		// Collect raw text from all pages
		bodyText := cleanText(e.Text)
		if len(bodyText) > 5000 {
			bodyText = bodyText[:5000]
		}
		allText.WriteString(bodyText)
		allText.WriteString("\n---\n")

		// Identify about pages
		if strings.Contains(lowerURL, "about") || strings.Contains(lowerURL, "chi-siamo") || strings.Contains(lowerURL, "azienda") {
			if data.AboutText == "" {
				data.AboutText = bodyText
			}
		}

		// Identify team pages
		if strings.Contains(lowerURL, "team") || strings.Contains(lowerURL, "people") || strings.Contains(lowerURL, "staff") {
			e.ForEach("h2, h3, h4, .team-member, .member-name", func(_ int, el *colly.HTMLElement) {
				name := strings.TrimSpace(el.Text)
				if name != "" && len(name) < 80 {
					data.TeamMembers = appendUnique(data.TeamMembers, name)
				}
			})
		}

		// Identify contact pages
		if strings.Contains(lowerURL, "contact") || strings.Contains(lowerURL, "contatti") {
			if data.ContactInfo == "" {
				data.ContactInfo = bodyText
			}
		}
	})

	// Detect technology stack from page source
	c.OnResponse(func(r *colly.Response) {
		body := string(r.Body)
		techSignals := map[string][]string{
			"React":      {"react", "__NEXT_DATA__", "next.js"},
			"Vue":        {"vue.js", "vuex", "nuxt"},
			"Angular":    {"ng-app", "angular"},
			"WordPress":  {"wp-content", "wordpress"},
			"Shopify":    {"shopify", "cdn.shopify.com"},
			"HubSpot":    {"hubspot", "hs-scripts"},
			"Salesforce": {"salesforce", "pardot"},
			"GA4":        {"gtag", "googletagmanager"},
			"Hotjar":     {"hotjar"},
			"Intercom":   {"intercom"},
			"Drift":      {"drift.com"},
			"Stripe":     {"stripe.com/v3"},
		}

		lowerBody := strings.ToLower(body)
		for tech, signals := range techSignals {
			for _, signal := range signals {
				if strings.Contains(lowerBody, signal) {
					data.TechStack = appendUnique(data.TechStack, tech)
					break
				}
			}
		}
	})

	// Follow internal links to about/team/contact pages
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		if visitedPages >= 10 {
			return
		}
		href := e.Attr("href")
		lowerHref := strings.ToLower(href)
		interesting := []string{"about", "team", "contact", "chi-siamo", "azienda", "contatti", "servizi", "services", "products", "prodotti"}
		for _, keyword := range interesting {
			if strings.Contains(lowerHref, keyword) {
				visitedPages++
				e.Request.Visit(href)
				break
			}
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		s.logger.Debug("scrape error", slog.String("url", r.Request.URL.String()), slog.String("error", err.Error()))
	})

	if err := c.Visit(targetURL); err != nil {
		return nil, fmt.Errorf("scrape %s: %w", targetURL, err)
	}
	c.Wait()

	// Cap raw text to avoid huge LLM prompts
	rawText := allText.String()
	if len(rawText) > 15000 {
		rawText = rawText[:15000]
	}
	data.RawText = rawText

	// Try to extract industry from description
	if data.Industry == "" && data.Description != "" {
		data.Industry = inferIndustry(data.Description)
	}

	s.logger.Info("scrape completed",
		slog.String("url", targetURL),
		slog.String("company", data.CompanyName),
		slog.Int("pages", visitedPages),
		slog.Int("techStack", len(data.TechStack)),
		slog.Int("socialLinks", len(data.SocialLinks)),
	)

	return data, nil
}

func appendUnique(slice []string, item string) []string {
	for _, existing := range slice {
		if strings.EqualFold(existing, item) {
			return slice
		}
	}
	return append(slice, item)
}

func cleanText(s string) string {
	// Collapse whitespace runs
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func inferIndustry(description string) string {
	lower := strings.ToLower(description)
	industries := map[string][]string{
		"Technology":      {"software", "saas", "tech", "digital", "cloud", "platform", "app"},
		"Manufacturing":   {"manufacturing", "produzione", "industrial", "factory"},
		"Finance":         {"finance", "fintech", "banking", "insurance", "assicura"},
		"Healthcare":      {"health", "medical", "pharma", "sanit"},
		"E-commerce":      {"ecommerce", "e-commerce", "shop", "store", "retail"},
		"Consulting":      {"consulting", "consulen", "advisory"},
		"Construction":    {"construction", "edil", "building", "costruz"},
		"Food & Beverage": {"food", "beverage", "ristoraz", "alimentar"},
		"Logistics":       {"logistics", "logistic", "transport", "spediz"},
		"Education":       {"education", "formazione", "training", "school"},
	}
	for industry, keywords := range industries {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				return industry
			}
		}
	}
	return ""
}
