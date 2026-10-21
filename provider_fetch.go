package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// providerClient is the HTTP client for Test Connection and Refresh.
// Tests that need a fake response replace providerFetch.
var providerClient = &http.Client{Timeout: 20 * time.Second}

// providerFetch, when set, replaces the network call. Tests use it to
// inject a failure or a model list without a server.
var providerFetch func(base, key, format string) (ids []string, status int, unsupported bool, err error)

const anthropicVersion = "2023-06-01"

func setModelListAuth(req *http.Request, key, format string) {
	key = strings.TrimSpace(key)
	if key == "" || req == nil {
		return
	}
	if normalizeFormat(format) == formatAnthropic && strings.TrimSpace(format) != "" {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", anthropicVersion)
		return
	}
	if req.URL != nil && strings.Contains(strings.ToLower(req.URL.Host), "openai.azure.com") {
		req.Header.Set("api-key", key)
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
}

func modelsEndpoint(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("Set a base URL first.")
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/models") {
		return base, nil
	}
	return base + "/models", nil
}

func fetchModelIDs(base, key, format string) (ids []string, status int, unsupported bool, err error) {
	if providerFetch != nil {
		return providerFetch(base, key, format)
	}
	endpoint, err := modelsEndpoint(base)
	if err != nil {
		return nil, 0, false, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Cove/"+appVersion)
	setModelListAuth(req, key, format)
	resp, err := providerClient.Do(req)
	if err != nil {
		return nil, 0, false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	status = resp.StatusCode
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented {
		return nil, status, true, nil
	}
	if status < 200 || status >= 300 {
		msg := strings.TrimSpace(string(body))
		if extracted := errorMessage(body); extracted != "" {
			msg = extracted
		}
		msg = strings.Join(strings.Fields(msg), " ")
		if msg == "" {
			msg = resp.Status
		}
		return nil, status, false, errors.New(msg)
	}
	ids = parseModelList(body)
	if len(ids) == 0 {
		return nil, status, true, nil
	}
	return ids, status, false, nil
}

func parseModelList(body []byte) []string {
	var wrap struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &wrap) == nil {
		var out []string
		seen := map[string]bool{}
		add := func(id string) {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				return
			}
			seen[id] = true
			out = append(out, id)
		}
		for _, m := range wrap.Data {
			add(m.ID)
		}
		for _, m := range wrap.Models {
			if m.ID != "" {
				add(m.ID)
			} else {
				add(m.Name)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	var bare []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &bare) == nil {
		var out []string
		for _, m := range bare {
			if strings.TrimSpace(m.ID) != "" {
				out = append(out, strings.TrimSpace(m.ID))
			}
		}
		return out
	}
	return nil
}

func (a *App) runProviderTest(now time.Time) {
	if a.provEdit.testing {
		return
	}
	if a.provEdit.custom {
		if httpURLError(a.provEdit.baseURL) != "" {
			return
		}
	} else if a.provEdit.name == "" || (!a.provEdit.keyless && strings.TrimSpace(a.provEdit.apiKey) == "") {
		return
	}
	name := a.provEdit.name
	id := a.provEdit.connID
	kind := name
	if a.provEdit.custom {
		kind = "Custom"
	}
	base := effectiveBase(kind, a.provEdit.baseURL)
	key := strings.TrimSpace(a.provEdit.apiKey)
	format := a.provEdit.format
	a.provEdit.testing = true
	a.provEdit.testKind = "run"
	a.provEdit.testLine = "Testing…"
	a.provEdit.refreshNote = ""
	start := time.Now()
	a.launch(func() {
		ids, status, unsupported, err := fetchModelIDs(base, key, format)
		ms := time.Since(start).Milliseconds()
		if ms < 1 {
			ms = 1
		}
		a.ui(func() {
			a.finishProviderTest(name, id, ids, status, unsupported, err, ms, now)
		})
	})
}

func (a *App) finishProviderTest(name, id string, ids []string, status int, unsupported bool, err error, ms int64, now time.Time) {
	c := a.connByID(id)
	if c == nil {
		c = a.providerConnection(name)
	}
	line := ""
	kind := "fail"
	probe := probeInvalid
	// A server with no /models list is not a rejected key. Leave the
	// saved status alone and only show the inline result.
	setProbe := true
	switch {
	case err == nil && !unsupported:
		kind = "ok"
		probe = probeOK
		line = fmt.Sprintf("Connected · %d models · %d ms", len(ids), ms)
	case unsupported:
		setProbe = false
		line = "This server doesn’t list its models."
	case status == 401 || status == 403:
		line = fmt.Sprintf("%d Unauthorized — the API key was rejected", status)
		if status == 403 {
			line = "403 Forbidden — the API key was rejected"
		}
	default:
		msg := "connection failed"
		if err != nil {
			msg = err.Error()
		}
		line = msg
	}
	if a.provEdit.draft && a.provEdit.name == name {
		a.provEdit.testing = false
		a.provEdit.testKind = kind
		a.provEdit.testLine = line
		if a.provEdit.custom && !a.provEdit.listed && kind == "ok" {
			if models := modelsEnabled(ids); len(models) > 0 {
				a.provEdit.models = models
				a.provEdit.listed = true
			}
		}
		return
	}
	if setProbe && c == nil && a.provEdit.name == name {
		c = a.ensureEditingConnection()
	}
	if setProbe && c != nil {
		// A custom provider's first successful list is the catalog the
		// user just added, so every id starts on. A later Test Connection
		// leaves that list alone. Refresh is what adds new ids, off.
		if c.Custom && !c.ModelsSet && kind == "ok" {
			if models := modelsEnabled(ids); len(models) > 0 {
				c.Models = models
				c.ModelsSet = true
				if a.provEdit.connID == c.ID {
					a.provEdit.models = append([]ProviderModel(nil), models...)
				}
			}
		}
		c.Probe = probe
		a.save()
	}
	if a.provEdit.name == name {
		a.provEdit.testing = false
		a.provEdit.testKind = kind
		a.provEdit.testLine = line
		if kind == "ok" {
			a.noteSaved(now)
		}
	}
}

func (a *App) refreshProviderModels(now time.Time) {
	if a.provEdit.name == "" || a.provEdit.refreshing {
		return
	}
	name := a.provEdit.name
	custom := a.provEdit.custom
	kind := name
	if custom {
		kind = "Custom"
	}
	base := effectiveBase(kind, a.provEdit.baseURL)
	key := strings.TrimSpace(a.provEdit.apiKey)
	format := a.provEdit.format
	existing := append([]ProviderModel(nil), a.provEdit.models...)
	first := false
	if custom {
		c := a.connByID(a.provEdit.connID)
		first = (c == nil || !c.ModelsSet) && !a.provEdit.listed
	}
	a.provEdit.refreshing = true
	a.provEdit.refreshNote = ""
	a.launch(func() {
		ids, _, unsupported, err := fetchModelIDs(base, key, format)
		a.ui(func() {
			a.finishRefresh(name, custom, first, existing, ids, unsupported, err, now)
		})
	})
}

func (a *App) finishRefresh(name string, custom, first bool, existing []ProviderModel, fetched []string, unsupported bool, err error, now time.Time) {
	if a.provEdit.name == name {
		a.provEdit.refreshing = false
	}
	if err != nil || unsupported {
		note := refreshFailure(name, custom, unsupported, err)
		if a.provEdit.name == name {
			a.provEdit.refreshNote = note
			if len(a.provEdit.models) == 0 {
				if custom {
					a.provEdit.models = existing
				} else {
					a.provEdit.models = builtinModels(name)
				}
			}
		}
		return
	}
	merged := mergeFetched(existing, fetched, first)
	if a.provEdit.name == name {
		a.provEdit.models = merged
		a.provEdit.refreshNote = ""
		if custom && first && len(merged) > 0 {
			a.provEdit.listed = true
		}
		a.writeEditorModels(now)
	}
}

func refreshFailure(name string, custom, unsupported bool, err error) string {
	if unsupported {
		if custom {
			return name + " doesn’t list its models. Showing your saved list — add models by ID."
		}
		return name + " doesn’t list its models. Showing the built-in list."
	}
	msg := "connection failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		msg = err.Error()
	}
	if custom {
		return "Couldn’t load models (" + msg + "). Showing your saved list."
	}
	return "Couldn’t load models (" + msg + "). Showing the built-in list."
}

func modelsEnabled(ids []string) []ProviderModel {
	var out []ProviderModel
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, ProviderModel{ID: id, Enabled: true})
	}
	return out
}

func mergeFetched(existing []ProviderModel, fetched []string, first bool) []ProviderModel {
	if first {
		return modelsEnabled(fetched)
	}
	out := append([]ProviderModel(nil), existing...)
	seen := map[string]bool{}
	for _, m := range out {
		seen[m.ID] = true
	}
	for _, id := range fetched {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, ProviderModel{ID: id, Enabled: false, Fresh: true})
	}
	return out
}
