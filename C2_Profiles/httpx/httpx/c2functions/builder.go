package c2functions

import (
	"MyContainer/httpx/mythicGraphql"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	c2structs "github.com/MythicMeta/MythicContainer/c2_structs"
	"github.com/MythicMeta/MythicContainer/logging"
	"github.com/MythicMeta/MythicContainer/utils/sharedStructs"
	"github.com/pelletier/go-toml"
	"golang.org/x/exp/slices"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	Instances []instanceConfig `json:"instances"`
}
type instanceConfig struct {
	Port             int               `json:"port"`
	KeyPath          string            `json:"key_path"`
	CertPath         string            `json:"cert_path"`
	Debug            bool              `json:"debug"`
	UseSSL           bool              `json:"use_ssl"`
	PayloadHostPaths map[string]string `json:"payloads"`
	BindIP           string            `json:"bind_ip"`
}
type AgentVariationConfigMessageTransform struct {
	Action string `json:"action" toml:"action"`
	Value  string `json:"value" toml:"value"`
}
type AgentVariationConfigMessage struct {
	Location string `json:"location" toml:"location"`
	Name     string `json:"name" toml:"name"`
}
type AgentVariationConfigClient struct {
	Headers               map[string]string                      `json:"headers" toml:"headers"`
	Parameters            map[string]string                      `json:"parameters" toml:"parameters"`
	DomainSpecificHeaders map[string]map[string]string           `json:"domain_specific_headers" toml:"domain_specific_headers"`
	Message               AgentVariationConfigMessage            `json:"message" toml:"message"`
	Transforms            []AgentVariationConfigMessageTransform `json:"transforms" toml:"transforms"`
}
type AgentVariationConfigServer struct {
	Headers    map[string]string                      `json:"headers" toml:"headers"`
	Transforms []AgentVariationConfigMessageTransform `json:"transforms" toml:"transforms"`
}
type AgentVariationConfig struct {
	Verb   string                     `json:"verb" toml:"verb"`
	URIs   []string                   `json:"uris" toml:"uris"`
	Client AgentVariationConfigClient `json:"client" toml:"client"`
	Server AgentVariationConfigServer `json:"server" toml:"server"`
}
type AgentVariations struct {
	Name string               `json:"name" toml:"name"`
	Get  AgentVariationConfig `json:"get" toml:"get"`
	Post AgentVariationConfig `json:"post" toml:"post"`
}

func getC2JsonConfig() (*config, error) {
	currentConfig := config{}
	if configBytes, err := os.ReadFile(filepath.Join(".", "httpx", "c2_code", "config.json")); err != nil {
		return nil, err
	} else if err = json.Unmarshal(configBytes, &currentConfig); err != nil {
		logging.LogError(err, "Failed to unmarshal config bytes")
		return nil, err
	} else {
		return &currentConfig, nil
	}
}
func writeC2JsonConfig(cfg *config) error {
	jsonBytes, err := json.MarshalIndent(*cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(".", "httpx", "c2_code", "config.json"), jsonBytes, 644)
}
func getAgentJsonConfig() (map[string]AgentVariations, error) {
	currentConfig := map[string]AgentVariations{}
	if configBytes, err := os.ReadFile(filepath.Join(".", "httpx", "c2_code", "agent_configs.json")); err != nil {
		return nil, err
	} else if len(strings.TrimSpace(string(configBytes))) == 0 {
		return currentConfig, nil
	} else if err = json.Unmarshal(configBytes, &currentConfig); err != nil {
		logging.LogError(err, "Failed to unmarshal config bytes")
		return nil, err
	} else {
		return currentConfig, nil
	}
}
func writeAgentJsonConfig(cfg map[string]AgentVariations) error {
	jsonBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(".", "httpx", "c2_code", "agent_configs.json"), jsonBytes, 644)
}

type rawC2ConfigPreset struct {
	Filename string `json:"filename"`
	Label    string `json:"label"`
	Content  string `json:"content"`
}

func listRawC2ConfigPresets() ([]rawC2ConfigPreset, error) {
	dir := filepath.Join(".", "httpx", "c2_code", "presets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []rawC2ConfigPreset{}, nil
		}
		return nil, err
	}
	presets := make([]rawC2ConfigPreset, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".json" && ext != ".toml" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			logging.LogError(err, "Failed to read preset", "file", entry.Name())
			continue
		}
		label := strings.TrimSuffix(entry.Name(), ext)
		if variation, err := parseInlineAgentVariation(string(content)); err == nil && variation.Name != "" && !strings.HasPrefix(variation.Name, "inline_") {
			label = variation.Name
		}
		presets = append(presets, rawC2ConfigPreset{
			Filename: entry.Name(),
			Label:    label,
			Content:  string(content),
		})
	}
	return presets, nil
}

func defaultAgentVariation(name string) AgentVariations {
	return AgentVariations{
		Name: name,
		Post: AgentVariationConfig{
			Verb: "POST",
			URIs: []string{"/"},
			Client: AgentVariationConfigClient{
				Message: AgentVariationConfigMessage{
					Location: "body",
				},
			},
		},
	}
}

func parseInlineAgentVariation(agentConfig string) (AgentVariations, error) {
	normalizedConfig := strings.TrimSpace(agentConfig)
	nameHash := sha256.Sum256([]byte(normalizedConfig))
	return parseAgentVariation([]byte(normalizedConfig), fmt.Sprintf("inline_%x", nameHash[:6]))
}

func rawC2ConfigSchema() map[string]interface{} {
	locations := []string{"body", "cookie", "query", "header"}
	transformActions := []string{"base64", "base64url", "netbios", "netbiosu", "xor", "prepend", "append"}
	transformActionsWithArg := []string{"xor", "prepend", "append"}

	transformItem := map[string]interface{}{
		"type": "object",
		"fields": []interface{}{
			map[string]interface{}{
				"name":    "action",
				"type":    "enum",
				"choices": transformActions,
				"label":   "Action",
			},
			map[string]interface{}{
				"name":      "value",
				"type":      "string",
				"label":     "Argument",
				"show_when": map[string]interface{}{"field": "action", "in": transformActionsWithArg},
				"placeholder_when": map[string]interface{}{
					"field": "action",
					"map": map[string]interface{}{
						"xor":     "key",
						"prepend": "text to prepend",
						"append":  "text to append",
					},
				},
			},
		},
	}

	messageField := map[string]interface{}{
		"name":  "message",
		"type":  "object",
		"label": "Message Placement",
		"fields": []interface{}{
			map[string]interface{}{"name": "location", "type": "enum", "choices": locations, "label": "Location"},
			map[string]interface{}{
				"name":      "name",
				"type":      "string",
				"label":     "Parameter Name",
				"show_when": map[string]interface{}{"field": "location", "in": []string{"cookie", "query", "header"}},
				"placeholder_when": map[string]interface{}{
					"field": "location",
					"map": map[string]interface{}{
						"cookie": "cookie name",
						"query":  "query parameter name",
						"header": "header name",
					},
				},
			},
		},
	}

	clientFields := []interface{}{
		messageField,
		map[string]interface{}{"name": "headers", "type": "string_map", "label": "HTTP Headers", "key_label": "Header", "value_label": "Value"},
		map[string]interface{}{"name": "parameters", "type": "string_map", "label": "Query Parameters", "key_label": "Name", "value_label": "Value"},
		map[string]interface{}{"name": "transforms", "type": "array", "label": "Transforms", "items": transformItem},
	}

	serverFields := []interface{}{
		map[string]interface{}{"name": "headers", "type": "string_map", "label": "HTTP Headers", "key_label": "Header", "value_label": "Value"},
		map[string]interface{}{"name": "transforms", "type": "array", "label": "Transforms", "items": transformItem},
	}

	buildRequestBlock := func(fieldName, label string, verbChoices []string) map[string]interface{} {
		return map[string]interface{}{
			"name":  fieldName,
			"type":  "object",
			"label": label,
			"fields": []interface{}{
				map[string]interface{}{"name": "verb", "type": "enum", "choices": verbChoices, "label": "HTTP Verb"},
				map[string]interface{}{"name": "uris", "type": "array", "label": "URI Paths", "items": map[string]interface{}{"type": "string"}},
				map[string]interface{}{"name": "client", "type": "object", "label": "Client → Server", "fields": clientFields},
				map[string]interface{}{"name": "server", "type": "object", "label": "Server → Client", "fields": serverFields},
			},
		}
	}

	return map[string]interface{}{
		"type":  "object",
		"label": "Agent Variation",
		"fields": []interface{}{
			map[string]interface{}{"name": "name", "type": "string", "label": "Name"},
			buildRequestBlock("get", "GET Request", []string{"GET"}),
			buildRequestBlock("post", "POST Request", []string{"POST", "PUT", "PATCH"}),
		},
	}
}

func parseAgentVariation(content []byte, fallbackName string) (AgentVariations, error) {
	if len(strings.TrimSpace(string(content))) == 0 {
		return defaultAgentVariation(fallbackName), nil
	}

	agentVariation := AgentVariations{}
	err := json.Unmarshal(content, &agentVariation)
	if err != nil {
		err2 := toml.Unmarshal(content, &agentVariation)
		if err2 != nil {
			return AgentVariations{}, errors.New(fmt.Sprintf("Error parsing agent config: %v\n%v\n", err, err2))
		}
	}
	if agentVariation.Name == "" {
		agentVariation.Name = fallbackName
	}
	if agentVariation.Post.Verb == "" {
		agentVariation.Post.Verb = "POST"
	}
	if len(agentVariation.Post.URIs) == 0 {
		agentVariation.Post.URIs = []string{"/"}
	}
	if agentVariation.Post.Client.Message.Location == "" {
		agentVariation.Post.Client.Message.Location = "body"
	}
	return agentVariation, nil
}

var validLocations = []string{"cookie", "query", "header", "body", ""}
var validActions = []string{"base64", "base64url", "netbios", "netbiosu", "xor", "prepend", "append"}
var version = "0.0.4"
var httpxc2definition = c2structs.C2Profile{
	Name:             "httpx",
	Author:           "@its_a_feature_",
	Description:      fmt.Sprintf("Crowdsourced and community driven HTTP profile with lots of variation options. Version: %s", version),
	IsP2p:            false,
	IsServerRouted:   true,
	ServerBinaryPath: filepath.Join(".", "httpx", "c2_code", "mythic_httpx_server"),
	ConfigCheckFunction: func(ctx context.Context, message c2structs.C2ConfigCheckMessage) c2structs.C2ConfigCheckMessageResponse {
		response := c2structs.C2ConfigCheckMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called config check\n%v", message),
		}
		// this is where we will need to update the config with what the agent supplied
		// this is called each time a new payload is created, so update the server's config with the agent's config
		agentConfig, err := message.GetStringArg("raw_c2_config")
		if err != nil {
			agentConfig = ""
		}
		err = validateAndUpdateConfig(ctx, agentConfigFileID)
		if err != nil {
			response.Success = false
			response.Error = err.Error()
			return response
		}
		response.Message = "Everything matches and looks good!"
		response.RestartInternalServer = true
		return response
	},
	GetRedirectorRulesFunction: func(ctx context.Context, message c2structs.C2GetRedirectorRuleMessage) c2structs.C2GetRedirectorRuleMessageResponse {
		response := c2structs.C2GetRedirectorRuleMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called redirector status check:\n%v", message),
		}
		agentConfig, err := message.GetStringArg("raw_c2_config")
		if err != nil {
			agentConfig = ""
		}
		agentVariation, err := parseInlineAgentVariation(agentConfig)
		agentConfigContents, err := mythicrpc.SendMythicRPCFileGetContent(ctx, mythicrpc.MythicRPCFileGetContentMessage{
			AgentFileID: agentConfigFileID,
		})
		if err != nil {
			response.Success = false
			response.Error += err.Error()
			return response
		}
		output := "#mod_rewrite rules generated from @AndrewChiles' project https://github.com/threatexpress/mythic2modrewrite:\n"
		getUA := ""
		for key, val := range agentVariation.Get.Client.Headers {
			if key == "User-Agent" {
				getUA = val
			}
		}
		postUA := ""
		for key, val := range agentVariation.Post.Client.Headers {
			if key == "User-Agent" {
				postUA = val
			}
		}
		// Create UA in modrewrite syntax. No regex needed in UA string matching, but () characters must be escaped
		getUAString := strings.ReplaceAll(getUA, "(", "\\(")
		getUAString = strings.ReplaceAll(getUAString, ")", "\\)")
		postUAString := strings.ReplaceAll(postUA, "(", "\\(")
		postUAString = strings.ReplaceAll(postUAString, ")", "\\)")
		// Create URI string in modrewrite syntax. "*" are needed in regex to support GET and uri-append parameters on the URI
		geturisString := strings.Join(agentVariation.Get.URIs, ".*|") + ".*"
		posturisString := strings.Join(agentVariation.Post.URIs, ".*|") + ".*"
		c2RewriteOutput := []string{}
		currentConfig, err := getC2JsonConfig()
		if err != nil {
			logging.LogError(err, "Failed to get current json configuration")
			response.Error = "Failed to get current json configuration"
			response.Success = false
			return response
		}
		c2RewriteTemplate := "RewriteRule ^.*$ \"%s%%{REQUEST_URI}\" [P,L]"
		for _, instance := range currentConfig.Instances {
			if instance.UseSSL {
				serverURL := fmt.Sprintf("https://C2_SERVER_HERE:%d", instance.Port)
				c2RewriteOutput = append(c2RewriteOutput, fmt.Sprintf(c2RewriteTemplate, serverURL))
			} else {
				serverURL := fmt.Sprintf("http://C2_SERVER_HERE:%d", instance.Port)
				c2RewriteOutput = append(c2RewriteOutput, fmt.Sprintf(c2RewriteTemplate, serverURL))
			}
		}

		output += "#\tReplace 'C2_SERVER_HERE' with the IP/Domain address of where matching traffic should go\n"

		htaccessTemplate := `
########################################
## .htaccess START
RewriteEngine On
## C2 Traffic (HTTP-GET, HTTP-POST, HTTP-STAGER URIs)
## Logic: If a requested URI AND the User-Agent matches, proxy the connection to the Teamserver
## Consider adding other HTTP checks to fine tune the check.  (HTTP Cookie, HTTP Referer, HTTP Query String, etc)
## Refer to http://httpd.apache.org/docs/current/mod/mod_rewrite.html
%s
## Redirect all other traffic here
RewriteRule ^.*$ redirect/? [L,R=302]
## .htaccess END
########################################
		`
		htaccessConditionTemplate := `
## Only allow GET and POST methods to pass to the C2 server
RewriteCond %%{REQUEST_METHOD} ^(%s) [NC]
## Profile URIs
RewriteCond %%{REQUEST_URI} ^(%s)$
## Profile UserAgent
RewriteCond %%{HTTP_USER_AGENT} "%s"`
		allHtaccessConditions := ""
		if len(agentVariation.Get.URIs) > 0 {
			gethtaccessConditions := fmt.Sprintf(htaccessConditionTemplate, "GET", geturisString, getUAString)
			for _, c2Entry := range c2RewriteOutput {
				allHtaccessConditions += gethtaccessConditions + "\n" + c2Entry + "\n"
			}
		}
		if len(agentVariation.Post.URIs) > 0 {
			posthtaccessConditions := fmt.Sprintf(htaccessConditionTemplate, "POST", posturisString, postUAString)
			for _, c2Entry := range c2RewriteOutput {
				allHtaccessConditions += posthtaccessConditions + "\n" + c2Entry + "\n"
			}
		}
		htaccess := fmt.Sprintf(htaccessTemplate, allHtaccessConditions)
		output += "#\tReplace 'redirect' with the http(s) address of where non-matching traffic should go, ex: https://redirect.com\n"
		output += "\n" + htaccess
		response.Message = output
		return response
	},
	OPSECCheckFunction: func(ctx context.Context, message c2structs.C2OPSECMessage) c2structs.C2OPSECMessageResponse {
		response := c2structs.C2OPSECMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called opsec check:\nNot currently checking opsec considerations"),
		}
		return response

	},
	GetIOCFunction: func(ctx context.Context, message c2structs.C2GetIOCMessage) c2structs.C2GetIOCMessageResponse {
		response := c2structs.C2GetIOCMessageResponse{Success: true}
		agentConfig, err := message.GetStringArg("raw_c2_config")
		if err != nil {
			agentConfig = ""
		}
		agentVariation, err := parseInlineAgentVariation(agentConfig)
		agentConfigContents, err := mythicrpc.SendMythicRPCFileGetContent(ctx, mythicrpc.MythicRPCFileGetContentMessage{
			AgentFileID: agentConfigFileID,
		})
		if err != nil {
			response.Success = false
			response.Error += err.Error()
			return response
		}
		domains, err := message.GetArrayArg("callback_domains")
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting callback domains: %v\n%v", err, message)
			return response
		}
		for _, domain := range domains {
			for _, uri := range agentVariation.Get.URIs {
				queryParamString := ""
				queryParams := []string{}
				for paramKey, paramVal := range agentVariation.Get.Client.Parameters {
					queryParams = append(queryParams, fmt.Sprintf("%s=%s", paramKey, paramVal))
				}
				if agentVariation.Get.Client.Message.Location == "query" {
					queryParams = append(queryParams, fmt.Sprintf("%s=", agentVariation.Get.Client.Message.Name))
				}
				if len(queryParams) > 0 {
					queryParamString = fmt.Sprintf("?%s", strings.Join(queryParams, "&"))
				}
				response.IOCs = append(response.IOCs, c2structs.IOC{
					Type: "url",
					IOC:  fmt.Sprintf("%s%s%s", domain, uri, queryParamString),
				})
			}
			for _, uri := range agentVariation.Post.URIs {
				queryParamString := ""
				queryParams := []string{}
				for paramKey, paramVal := range agentVariation.Post.Client.Parameters {
					queryParams = append(queryParams, fmt.Sprintf("%s=%s", paramKey, paramVal))
				}
				if agentVariation.Post.Client.Message.Location == "query" {
					queryParams = append(queryParams, fmt.Sprintf("%s=", agentVariation.Post.Client.Message.Name))
				}
				if len(queryParams) > 0 {
					queryParamString = fmt.Sprintf("?%s", strings.Join(queryParams, "&"))
				}
				response.IOCs = append(response.IOCs, c2structs.IOC{
					Type: "url",
					IOC:  fmt.Sprintf("%s%s%s", domain, uri, queryParamString),
				})
			}
		}
		return response
	},
	SampleMessageFunction: func(ctx context.Context, message c2structs.C2SampleMessageMessage) c2structs.C2SampleMessageResponse {
		response := c2structs.C2SampleMessageResponse{Success: true, Message: "\n"}
		agentConfig, err := message.GetStringArg("raw_c2_config")
		if err != nil {
			agentConfig = ""
		}
		agentVariation, err := parseInlineAgentVariation(agentConfig)
		agentConfigContents, err := mythicrpc.SendMythicRPCFileGetContent(ctx, mythicrpc.MythicRPCFileGetContentMessage{
			AgentFileID: agentConfigFileID,
		})
		if err != nil {
			response.Success = false
			response.Error += err.Error()
			return response
		}
		domains, err := message.GetArrayArg("callback_domains")
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting callback domains: %v\n%v", err, message)
			return response
		}
		if len(domains) == 0 {
			response.Success = false
			response.Error += fmt.Sprintf("No callback domains specified")
			return response
		}
		sampleGetMessage := "YmRhZWMyNzgtYjA3Ny00YTYwLWJiOTgtNDgyOTY3NzRiZTk55+6GlUoN2rVpeXWuGH8zbhq91ggbx4jSr8d7YDgI4JuZMmakeO/FWYjfq1DtBaBR3W8/4Sof39X+LAxlbmCbdUKEC6T/TUR1JU09nHAZZKp/ijSXwRyOVvWX43rV1WVFQrU/VHcLXdjSN4lmh/HZPLeXXjQaqzFdPpiZNUzKJBE="
		samplePostMessage := "MjE1YTAzMzYtNDIwZS00ODQ1LTkzYzgtMmZjMTk0NjM3MTg1PMT1n1AgPfAILertsFNKCLZ5hcNokXHLlhDhcqlQ3DjV7xpk/P3IR2Li68DXLbr63r/nKRUv1Ii4Ouuz/5ZA5iqs4zv4jF3XRvvUfBaBeKkWZmjORVn3+ZhRHVeljLqvA6ejhCozfrDEO/SBgnahZbCShYW38rGvGkzYwdfamSOpH2XK8RjXh91VTrU3JbODYnV+WifL3qbyNQAdQWcC5M+G/hDsR9so/46c+HUPCDbJtrSmtDZqMf5NPsNcjyshhTwNtQ9/i41PFmKb5YkU5QaT5THHDPvdleYW2IxsZNxhyRlFy9ZxlAtoEyw6OYZx3ESdkZDxCGHCZiNFGFY3WJGo74I0Ips2F94IgQazghIhhv8yp7O5Aw+icLPkKI2l9xgvGhadiAUIfn90ynI0YeByH7KPe79WsiIVNtL4RKbs35BBYcJ5SV+HNPnDAXv7CF5pJCsVGS6jdsWaeoBoMaO7dUKdXvjd8P7F92uXrTQws3HvrJXCs1t53H9hz3orkuvSCcUGU0rmduuFTNbUYbcg544nooJLbXEX0/sMaPNvI2YyZ3+U7nLeKgc4zwaGGohQVJBbXN07lMqunBiOA41nFzpz9BQA2pu9g6aRyKv2hllad2T+xvW1285oQnbi1DG6p5LHSBJuLuziGKKaPEL4p7a5owRalbUhu6S1J0aNHvazHjzw4yZtOCpzV9hRtDTGbTn3oSMQ4xKaG9M/GeQEK44MH77UkogqF/u9AXDzvnaEt2vnp/d//b/oKWr0/JZKLfVmI/GlUFT+DCa2pA4nW888EUwRLYkauh0qmC5NdZ3oS9imX9SbOR8XAJGePN1ccfJNLwqljmjyETbgKwBjLyisJ+AuGnXkA/vO6kBhCrkrIl7iDym9YMGvby79tFNgPI/Q3CmZucPPsL0NL4QRiMsvxDevfyDUAHftaKbPvxgIPq4gGOhwDVfhUM8TTsSuWSZVK8RVYyJWw+MXXxve41IQw/mcEF/bZzSUUqJdgXwd4WDaXl/WTSQlxZbBep7SxdScQtL4M2opkoZEdOfKtb1Ywe/AF81oA+VSZEF7GIXnBO0xv3kfXmuOEfCD0ej3M0CsIESrkovv4vCgof9YKelwJ91lO+/SbvsmYkf+b+nZ6uK85vqZPaSYvXzuqEq+ZO2KiYE+6Uui1CbwhudRw6WExpzHtsBfVDTJ5soLUM622CHT2NSa8Z46SqOKihycg49Pcp7eQs95l4KLs9Oi2VLZ4VpiFiXCdLFaJPkOfAHH5aCJTUIE3asBvcP04HNaUr/jahSJtL+p5Nt5UrbehnpW+e377YAVwk5UxmILrQ8HoLAz54zCcsEY/oXxrotNCMqG/LWT81mVqr74xR/Wgh4SDz8NET/emjEwbfC750/Mhpc/X4MW0pijmxNlTXzu4B0gtgNW9dSBwoNwId46AvXXPgXipzvPj60w/Xz7srC8Vt0/dIEBPHp2tU4EjI6WUxYrcTpUahfsVysG8BcaH4oNWwpqPK6GMnBkMMfE6hQfyFzC0i3LiaFq1AL1J5CMt5IZFWJqCGFqq0YjoA=="
		agentMessageGetTransformed, err := transformMessageToClientRequest([]byte(sampleGetMessage), agentVariation.Get)
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating client get request: %v\n", err)
			return response
		}
		agentMessagePostTransformed, err := transformMessageToClientRequest([]byte(samplePostMessage), agentVariation.Post)
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating client post request: %v\n", err)
			return response
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		client := server.Client()
		if len(agentVariation.Get.URIs) > 0 {
			// get message
			reqGet, err := http.NewRequest(agentVariation.Get.Verb, domains[0]+agentVariation.Get.URIs[0], nil)
			if err != nil {
				response.Success = false
				response.Error += fmt.Sprintf("Error simulating client get request: %v\n", err)
				return response
			}
			q := reqGet.URL.Query()
			switch agentVariation.Get.Client.Message.Location {
			case "cookie":
				reqGet.AddCookie(&http.Cookie{
					Name:  agentVariation.Get.Client.Message.Name,
					Value: string(agentMessageGetTransformed),
				})
			case "query":
				q.Add(agentVariation.Get.Client.Message.Name, string(agentMessageGetTransformed))
			case "header":
				reqGet.Header.Set(agentVariation.Get.Client.Message.Name, string(agentMessageGetTransformed))
			default:
				// do nothing, it's the body and we already added it
			}
			for key := range agentVariation.Get.Client.Headers {
				if key == "Host" {
					reqGet.Host = agentVariation.Get.Client.Headers[key]
				} else if key == "User-Agent" {
					reqGet.Header.Set(key, agentVariation.Get.Client.Headers[key])
				} else if key == "Content-Length" {
					continue
				} else {
					reqGet.Header.Set(key, agentVariation.Get.Client.Headers[key])
				}
			}
			for key := range agentVariation.Get.Client.Parameters {
				q.Add(key, agentVariation.Get.Client.Parameters[key])
			}
			reqGet.URL.RawQuery = q.Encode()
			dump, err := httputil.DumpRequest(reqGet, true)
			response.Message += "GET Variation Client Message:\n" + fmt.Sprintf("%s\n\n", dump)
			serverResp, err := client.Get(server.URL)
			if err != nil {
				response.Success = false
				response.Error += fmt.Sprintf("Error simulating server get response: %v\n\n", err)
				return response
			}
			for key, val := range agentVariation.Get.Server.Headers {
				serverResp.Header.Set(key, val)
			}
			serverResp.Body.Close()
			agentMessage, err := transformMessageFromServer([]byte(samplePostMessage), agentVariation.Get)
			if err != nil {
				logging.LogError(err, "failed to create transformed response for agent")
				response.Success = false
				response.Error += fmt.Sprintf("Error simulating server get response: %v\n\n", err)
				return response
			}
			serverResp.Body = io.NopCloser(bytes.NewBuffer(agentMessage))
			serverResp.ContentLength = int64(len(agentMessage))
			dump, err = httputil.DumpResponse(serverResp, true)
			response.Message += "GET Variation Server Response:\n" + fmt.Sprintf("%s\n\n", dump)
		}
		// post message
		var bodyBuffer *bytes.Buffer
		var bodyBytes []byte
		if slices.Contains([]string{"", "body"}, agentVariation.Post.Client.Message.Location) {
			bodyBytes = agentMessagePostTransformed
		} else {
			bodyBytes = make([]byte, 0)
		}
		bodyBuffer = bytes.NewBuffer(bodyBytes)
		reqPost, err := http.NewRequest(agentVariation.Post.Verb, domains[0]+agentVariation.Post.URIs[0], bodyBuffer)
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating client post request: %v\n", err)
			return response
		}
		qPost := reqPost.URL.Query()
		switch agentVariation.Post.Client.Message.Location {
		case "cookie":
			reqPost.AddCookie(&http.Cookie{
				Name:  agentVariation.Post.Client.Message.Name,
				Value: string(agentMessagePostTransformed),
			})
		case "query":
			qPost.Add(agentVariation.Post.Client.Message.Name, string(agentMessagePostTransformed))
		case "header":
			reqPost.Header.Set(agentVariation.Post.Client.Message.Name, string(agentMessagePostTransformed))
		default:
			// do nothing, it's the body and we already added it
		}
		for key, _ := range agentVariation.Post.Client.Headers {
			if key == "Host" {
				reqPost.Host = agentVariation.Post.Client.Headers[key]
			} else if key == "User-Agent" {
				reqPost.Header.Set(key, agentVariation.Post.Client.Headers[key])
			} else if key == "Content-Length" {
				continue
			} else {
				reqPost.Header.Set(key, agentVariation.Post.Client.Headers[key])
			}
		}
		// adding query parameters is a little weird in go

		for key, _ := range agentVariation.Post.Client.Parameters {
			qPost.Add(key, agentVariation.Post.Client.Parameters[key])
		}
		reqPost.URL.RawQuery = qPost.Encode()

		dumpPost, err := httputil.DumpRequest(reqPost, true)
		response.Message += "POST Variation Client Message:\n" + fmt.Sprintf("%s\n\n", dumpPost)
		serverResp, err := client.Post(server.URL, "application/text", bodyBuffer)
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating server post response: %v\n\n", err)
			return response
		}
		for key, val := range agentVariation.Post.Server.Headers {
			serverResp.Header.Set(key, val)
		}
		serverResp.Body.Close()
		agentMessage, err := transformMessageFromServer([]byte(samplePostMessage), agentVariation.Post)
		if err != nil {
			logging.LogError(err, "failed to create transformed response for agent")
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating server post response: %v\n", err)
			return response
		}
		serverResp.Body = io.NopCloser(bytes.NewBuffer(agentMessage))
		serverResp.ContentLength = int64(len(agentMessage))
		dump, err := httputil.DumpResponse(serverResp, true)
		response.Message += "POST Variation Server Response:\n" + fmt.Sprintf("%s\n\n", dump)
		return response
	},
	HostFileFunction: func(ctx context.Context, message c2structs.C2HostFilesMessage) c2structs.C2HostFilesMessageResponse {
		response := c2structs.C2HostFilesMessageResponse{
			Success: true,
			Results: make([]c2structs.C2HostFileMessageResponse, 0, len(message.Files)),
		}
		config, err := getC2JsonConfig()
		if err != nil {
			return c2structs.C2HostFilesMessageResponse{
				Success: false,
				Error:   err.Error(),
			}
		}
		for _, file := range message.Files {
			fileResponse := c2structs.C2HostFileMessageResponse{
				AgentFileID: file.AgentFileID,
				HostURL:     file.HostURL,
				Success:     true,
			}
			for i := range config.Instances {
				if config.Instances[i].PayloadHostPaths == nil {
					config.Instances[i].PayloadHostPaths = make(map[string]string)
				}
				if file.Remove {
					for hostURL, agentFileID := range config.Instances[i].PayloadHostPaths {
						if agentFileID == file.AgentFileID {
							delete(config.Instances[i].PayloadHostPaths, hostURL)
						}
					}
				} else {
					config.Instances[i].PayloadHostPaths[file.HostURL] = file.AgentFileID
				}
			}
			response.Results = append(response.Results, fileResponse)
		}
		err = writeC2JsonConfig(config)
		if err != nil {
			response.Success = false
			response.Error = err.Error()
			for i := range response.Results {
				response.Results[i].Success = false
				response.Results[i].Error = err.Error()
			}
			return response
		}
		response.RestartInternalServer = true
		return response
	},
	OnContainerStartFunction: func(ctx context.Context, message sharedStructs.ContainerOnStartMessage) sharedStructs.ContainerOnStartMessageResponse {
		response := sharedStructs.ContainerOnStartMessageResponse{}
		logging.LogInfo("called onStart function", "operation", message.OperationName)
		client := mythicGraphql.NewClient("https://127.0.0.1:7443/graphql/", message.APIToken)
		payloads, err := GetPayloadsQuery(ctx, client)
		if err != nil {
			response.EventLogErrorMessage = fmt.Sprintf("Failed to fetch payloads: %v\n", err)
			return response
		}
		for _, payload := range payloads.GetPayload() {
			for _, c2param := range payload.GetC2profileparametersinstances() {
				if c2param.C2profileparameter.Name == "raw_c2_config" {
					err = validateAndUpdateConfig(ctx, c2param.Value)
					if err != nil {
						response.EventLogErrorMessage += fmt.Sprintf("%s (%s) - %s:\n\t%s\n",
							payload.Filemetum.Filename_utf8,
							payload.Payloadtype.Name,
							payload.Description,
							err.Error())
					}
				}
			}
		}
		response.RestartInternalServer = true
		return response
	},
	CustomRPCFunctions: map[string]func(message c2structs.C2RPCOtherServiceRPCMessage) c2structs.C2RPCOtherServiceRPCMessageResponse{
		"list_raw_c2_config_presets": func(message c2structs.C2RPCOtherServiceRPCMessage) c2structs.C2RPCOtherServiceRPCMessageResponse {
			response := c2structs.C2RPCOtherServiceRPCMessageResponse{Result: map[string]interface{}{}}
			presets, err := listRawC2ConfigPresets()
			if err != nil {
				response.Success = false
				response.Error = err.Error()
				return response
			}
			response.Success = true
			response.Result["presets"] = presets
			return response
		},
	},
}
var httpxc2parameters = []c2structs.C2Parameter{
	{
		Name:          "callback_domains",
		DisplayName:   "Callback Domains",
		GroupName:     "Callbacks",
		Description:   "Array of callback domains as http(s)://host:port (e.g. https://example.com:443)",
		DefaultValue:  []string{"https://example.com:443"},
		ParameterType: c2structs.C2_PARAMETER_TYPE_ARRAY,
		Required:      true,
		UiPosition:    1,
	},
	{
		Name:          "domain_rotation",
		DisplayName:   "Domain Rotation",
		GroupName:     "Callbacks",
		Description:   "Domain rotation pattern. Fail-over uses each one in order until it can't communicate with it successfully and moves on. Round-robin makes each request to the next host in the list.",
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Choices: []string{
			"fail-over",
			"round-robin",
			"random",
		},
		ChoicesDisplayNames: map[string]string{
			"fail-over":   "Fail-over",
			"round-robin": "Round-robin",
			"random":      "Random",
		},
		UiPosition: 2,
	},
	{
		Name:          "failover_threshold",
		DisplayName:   "Failover Threshold",
		GroupName:     "Callbacks",
		Description:   "Domain fail-over threshold for how many times to keep trying one host before moving onto the next",
		DefaultValue:  5,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		UiPosition:    3,
	},
	{
		Name:          "callback_interval",
		DisplayName:   "Callback Interval",
		GroupName:     "Callbacks",
		Description:   "Callback Interval in seconds",
		DefaultValue:  10,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
		UiPosition:    6,
	},
	{
		Name:          "callback_jitter",
		DisplayName:   "Callback Jitter",
		GroupName:     "Callbacks",
		Description:   "Callback Jitter in percent",
		DefaultValue:  23,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
		UiPosition:    7,
	},
	{
		Name:          "killdate",
		DisplayName:   "Kill Date",
		GroupName:     "Callbacks",
		Description:   "Date when the agent should stop executing",
		DefaultValue:  365,
		ParameterType: c2structs.C2_PARAMETER_TYPE_DATE,
		Required:      false,
		UiPosition:    8,
	},
	{
		Name:          "AESPSK",
		DisplayName:   "AES PSK",
		GroupName:     "Crypto",
		Description:   "Encryption Type",
		DefaultValue:  "aes256_hmac",
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Required:      false,
		IsCryptoType:  true,
		Choices: []string{
			"aes256_hmac",
			"none",
		},
		ChoicesDisplayNames: map[string]string{
			"aes256_hmac": "AES-256 HMAC",
			"none":        "None",
		},
		UiPosition: 4,
	},
	{
		Name:          "encrypted_exchange_check",
		DisplayName:   "Encrypted Key Exchange",
		GroupName:     "Crypto",
		Description:   "Perform Key Exchange",
		DefaultValue:  true,
		ParameterType: c2structs.C2_PARAMETER_TYPE_BOOLEAN,
		Required:      false,
		UiPosition:    5,
	},
	{
		Name:          "proxy_host",
		DisplayName:   "Proxy Host",
		GroupName:     "Proxy",
		Description:   "Proxy host in format host:port",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		UiPosition:    9,
	},
	{
		Name:          "proxy_user",
		DisplayName:   "Proxy User",
		GroupName:     "Proxy",
		Description:   "Proxy username",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		UiPosition:    10,
	},
	{
		Name:          "proxy_pass",
		DisplayName:   "Proxy Password",
		GroupName:     "Proxy",
		Description:   "Proxy password",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		Required:      false,
		UiPosition:    11,
	},
	{
		Name:          "raw_c2_config",
		DisplayName:   "Raw C2 Config",
		GroupName:     "Advanced",
		Description:   "Inline agent configuration in JSON or TOML. Leave empty to use the default no-transform profile.",
		DefaultValue:  "",
		FormatString:  "ui:config_editor:json_toml:presets_fn=list_raw_c2_config_presets",
		ParameterType: c2structs.C2_PARAMETER_TYPE_STRING,
		FormSchema:    rawC2ConfigSchema(),
		Required:      false,
		UiPosition:    12,
	},
}

func validateAndUpdateConfig(agentConfig string) error {
	agentVariation, err := parseInlineAgentVariation(agentConfig)
func validateAndUpdateConfig(ctx context.Context, agentConfigFileID string) error {
	agentConfigContents, err := mythicrpc.SendMythicRPCFileGetContent(ctx, mythicrpc.MythicRPCFileGetContentMessage{
		AgentFileID: agentConfigFileID,
	})
	if err != nil {
		return err
	}
	if agentVariation.Name == "" {
		return errors.New(fmt.Sprintf("Missing name for agent variation"))
	}
	if !slices.Contains(validLocations, agentVariation.Get.Client.Message.Location) {
		return errors.New(fmt.Sprintf("Missing invalid message location for GET"))
	}
	if !slices.Contains([]string{"body", ""}, agentVariation.Get.Client.Message.Location) {
		if agentVariation.Get.Client.Message.Name == "" {
			return errors.New(fmt.Sprintf("Missing name for agent GET variation location"))
		}
	}
	if !slices.Contains(validLocations, agentVariation.Post.Client.Message.Location) {
		return errors.New(fmt.Sprintf("Missing invalid message location for POST"))
	}
	if !slices.Contains([]string{"body", ""}, agentVariation.Post.Client.Message.Location) {
		if agentVariation.Post.Client.Message.Name == "" {
			return errors.New(fmt.Sprintf("Missing name for agent POST variation location"))
		}
	}
	for i, _ := range agentVariation.Get.Client.Transforms {
		if !slices.Contains(validActions, agentVariation.Get.Client.Transforms[i].Action) {
			return errors.New(fmt.Sprintf("invalid client GET transform action: %s\n", agentVariation.Get.Client.Transforms[i].Action))
		}
	}
	for i, _ := range agentVariation.Get.Server.Transforms {
		if !slices.Contains(validActions, agentVariation.Get.Server.Transforms[i].Action) {
			return errors.New(fmt.Sprintf("invalid server GET transform action: %s\n", agentVariation.Get.Server.Transforms[i].Action))
		}
	}
	for i, _ := range agentVariation.Post.Client.Transforms {
		if !slices.Contains(validActions, agentVariation.Post.Client.Transforms[i].Action) {
			return errors.New(fmt.Sprintf("invalid client POST transform action: %s\n", agentVariation.Post.Client.Transforms[i].Action))
		}
	}
	for i, _ := range agentVariation.Post.Server.Transforms {
		if !slices.Contains(validActions, agentVariation.Post.Server.Transforms[i].Action) {
			return errors.New(fmt.Sprintf("invalid server POST transform action: %s\n", agentVariation.Post.Server.Transforms[i].Action))
		}
	}
	if len(agentVariation.Post.URIs) == 0 {
		return errors.New(fmt.Sprintf("Missing URIs array for agent POST variation"))
	}
	currentAgentConfig, err := getAgentJsonConfig()
	if err != nil {
		return errors.New(fmt.Sprintf("Error getting agent_configs.json: %v\n", err))
	}
	for name, _ := range currentAgentConfig {
		if name == agentVariation.Name {
			continue // allow overriding our own config name without issue
		}
		for _, uri := range agentVariation.Get.URIs {
			if slices.Contains(currentAgentConfig[name].Get.URIs, uri) {
				return errors.New(fmt.Sprintf("Config '%s' already uses Get URI '%s'! Aborting!\n\nIf '%s' is no longer needed, please delete all payloads that use it and remove it from 'agent_configs.json' through the C2 Profiles page and clicking the paperclip icon to edit files.", name, uri, name))
			}
			if slices.Contains(currentAgentConfig[name].Post.URIs, uri) {
				return errors.New(fmt.Sprintf("Config '%s' already uses Get URI '%s' in POST requests! Aborting!\n\nIf '%s' is no longer needed, please delete all payloads that use it and remove it from 'agent_configs.json' through the C2 Profiles page and clicking the paperclip icon to edit files.", name, uri, name))
			}
		}
		for _, uri := range agentVariation.Post.URIs {
			if slices.Contains(currentAgentConfig[name].Get.URIs, uri) {
				return errors.New(fmt.Sprintf("Config '%s' already uses Post URI '%s' in GET requests! Aborting!\n\nIf '%s' is no longer needed, please delete all payloads that use it and remove it from 'agent_configs.json' through the C2 Profiles page and clicking the paperclip icon to edit files.", name, uri, name))
			}
			if slices.Contains(currentAgentConfig[name].Post.URIs, uri) {
				return errors.New(fmt.Sprintf("Config '%s' already uses Post URI '%s'! Aborting!\n\nIf '%s' is no longer needed, please delete all payloads that use it and remove it from 'agent_configs.json' through the C2 Profiles page and clicking the paperclip icon to edit files.", name, uri, name))
			}
		}
	}
	currentAgentConfig[agentVariation.Name] = agentVariation
	err = writeAgentJsonConfig(currentAgentConfig)
	if err != nil {
		return errors.New(fmt.Sprintf("Error writing agent_configs.json: %v\n", err))
	}
	return nil
}
func Initialize() {
	c2structs.AllC2Data.Get("httpx").AddC2Definition(httpxc2definition)
	c2structs.AllC2Data.Get("httpx").AddParameters(httpxc2parameters)
}
