package router

import (
	"github.com/anthropics/anthropic-sdk-go"
)

type Domain struct {
	Article  string
	Database string
}

var (
	DomainEcommerce = Domain{Article: "an", Database: "ecommerce Postgres database"}
	DomainBird      = Domain{Article: "a", Database: "SQLite database from the BIRD benchmark"}
)

func (d Domain) orDefault() Domain {
	if d.Database == "" {
		return DomainEcommerce
	}
	if d.Article == "" {
		d.Article = "a"
	}
	return d
}

func (d Domain) indefinite() string {
	d = d.orDefault()
	return d.Article + " " + d.Database
}

func (d Domain) definite() string {
	return "the " + d.orDefault().Database
}

type PromptOptions struct {
	Domain  Domain
	NoCache bool
}

func systemBlocks(instructions, semanticText string, noCache bool) []anthropic.TextBlockParam {
	if noCache {
		return []anthropic.TextBlockParam{{Text: instructions}, {Text: semanticText}}
	}
	return []anthropic.TextBlockParam{
		{Text: instructions},
		{Text: semanticText, CacheControl: anthropic.NewCacheControlEphemeralParam()},
	}
}
