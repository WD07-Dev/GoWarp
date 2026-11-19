// Code generated from the extension Go comment for regression tests. DO NOT EDIT.
package extension

import (
	"fmt"
	e "gowarp/core/extension"
	"net/url"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "openlinkextension", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "openlinkextension_openNewTab", Kind: "command", Inputs: []string{"URL"}, Handler: func(c *e.Context) (e.Result, error) {
			link := c.String("URL")
			if !strings.HasPrefix(strings.ToLower(link), "https://") && !strings.HasPrefix(strings.ToLower(link), "http://") {
				link = "https://" + link
			}
			parsed, err := url.Parse(link)
			if err != nil || parsed.Hostname() == "" {
				return e.Result{}, fmt.Errorf("invalid extension link")
			}
			_, err = c.Call("host.command", "open-url", parsed.String())
			return e.Result{}, err
		}},
	}}
}
