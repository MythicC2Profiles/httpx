package c2functions

import (
	"bytes"
	"encoding/json"
	"fmt"
	c2structs "github.com/MythicMeta/MythicContainer/c2_structs"
	"github.com/MythicMeta/MythicContainer/logging"
	"github.com/MythicMeta/MythicContainer/mythicrpc"
	"github.com/pelletier/go-toml"
	"golang.org/x/exp/slices"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
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
	Headers    map[string]string                      `json:"headers" toml:"headers"`
	Parameters map[string]string                      `json:"parameters" toml:"parameters"`
	Message    AgentVariationConfigMessage            `json:"message" toml:"message"`
	Transforms []AgentVariationConfigMessageTransform `json:"transforms" toml:"transforms"`
}
type AgentVariationConfigServer struct {
	Headers    map[string]string                      `json:"headers" toml:"headers"`
	Transforms []AgentVariationConfigMessageTransform `json:"transforms" toml:"transforms"`
}
type AgentVariationConfig struct {
	Verb   string                     `json:"verb" toml:"verb"`
	URI    string                     `json:"uri" toml:"uri"`
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

var validLocations = []string{"cookie", "query", "header", "body", ""}
var validActions = []string{"base64", "base64url", "netbios", "netbiosu", "xor", "prepend", "append"}
var version = "0.0.1"
var httpxc2definition = c2structs.C2Profile{
	Name:             "httpx",
	Author:           "@its_a_feature_",
	Description:      fmt.Sprintf("CURRENTLY IN BETA! Crowdsourced and community driven HTTP profile with lots of variation options. Version: %s", version),
	IsP2p:            false,
	IsServerRouted:   true,
	ServerBinaryPath: filepath.Join(".", "httpx", "c2_code", "mythic_httpx_server"),
	ConfigCheckFunction: func(message c2structs.C2ConfigCheckMessage) c2structs.C2ConfigCheckMessageResponse {
		response := c2structs.C2ConfigCheckMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called config check\n%v", message),
		}
		// this is where we will need to update the config with what the agent supplied
		// this is called each time a new payload is created, so update the server's config with the agent's config
		agentConfigFileID, err := message.GetFileArg("raw_c2_config")
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %v\n", err)
			return response
		}
		agentConfigContents, err := mythicrpc.SendMythicRPCFileGetContent(mythicrpc.MythicRPCFileGetContentMessage{
			AgentFileID: agentConfigFileID,
		})
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %v\n", err)
			return response
		}
		if !agentConfigContents.Success {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %s\n", agentConfigContents.Error)
			return response
		}
		agentVariation := AgentVariations{}
		err = json.Unmarshal(agentConfigContents.Content, &agentVariation)
		if err != nil {
			err2 := toml.Unmarshal(agentConfigContents.Content, &agentVariation)
			if err2 != nil {
				response.Success = false
				response.Error += fmt.Sprintf("Error parsing agent config: %v\n%v\n", err, err2)
				return response
			}
		}
		if agentVariation.Name == "" {
			response.Success = false
			response.Error += fmt.Sprintf("Missing name for agent variation")
			return response
		}
		if !slices.Contains(validLocations, agentVariation.Get.Client.Message.Location) {
			response.Success = false
			response.Error += fmt.Sprintf("Missing invalid message location for GET")
			return response
		}
		if !slices.Contains([]string{"body", ""}, agentVariation.Get.Client.Message.Location) {
			if agentVariation.Get.Client.Message.Name == "" {
				response.Success = false
				response.Error += fmt.Sprintf("Missing name for agent GET variation location")
				return response
			}
		}
		if !slices.Contains(validLocations, agentVariation.Post.Client.Message.Location) {
			response.Success = false
			response.Error += fmt.Sprintf("Missing invalid message location for POST")
			return response
		}
		if !slices.Contains([]string{"body", ""}, agentVariation.Post.Client.Message.Location) {
			if agentVariation.Post.Client.Message.Name == "" {
				response.Success = false
				response.Error += fmt.Sprintf("Missing name for agent POST variation location")
				return response
			}
		}
		for i, _ := range agentVariation.Get.Client.Transforms {
			if !slices.Contains(validActions, agentVariation.Get.Client.Transforms[i].Action) {
				response.Success = false
				response.Error += fmt.Sprintf("invalid client GET transform action: %s\n", agentVariation.Get.Client.Transforms[i].Action)
				return response
			}
		}
		for i, _ := range agentVariation.Get.Server.Transforms {
			if !slices.Contains(validActions, agentVariation.Get.Server.Transforms[i].Action) {
				response.Success = false
				response.Error += fmt.Sprintf("invalid server GET transform action: %s\n", agentVariation.Get.Server.Transforms[i].Action)
				return response
			}
		}
		for i, _ := range agentVariation.Post.Client.Transforms {
			if !slices.Contains(validActions, agentVariation.Post.Client.Transforms[i].Action) {
				response.Success = false
				response.Error += fmt.Sprintf("invalid client POST transform action: %s\n", agentVariation.Post.Client.Transforms[i].Action)
				return response
			}
		}
		for i, _ := range agentVariation.Post.Server.Transforms {
			if !slices.Contains(validActions, agentVariation.Post.Server.Transforms[i].Action) {
				response.Success = false
				response.Error += fmt.Sprintf("invalid server POST transform action: %s\n", agentVariation.Post.Server.Transforms[i].Action)
				return response
			}
		}
		currentAgentConfig, err := getAgentJsonConfig()
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %v\n", err)
			return response
		}
		currentAgentConfig[agentVariation.Name] = agentVariation
		err = writeAgentJsonConfig(currentAgentConfig)
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %v\n", err)
			return response
		}
		return response
	},
	GetRedirectorRulesFunction: func(message c2structs.C2GetRedirectorRuleMessage) c2structs.C2GetRedirectorRuleMessageResponse {
		response := c2structs.C2GetRedirectorRuleMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called redirector status check:\n%v", message),
		}
		return response
	},
	OPSECCheckFunction: func(message c2structs.C2OPSECMessage) c2structs.C2OPSECMessageResponse {
		response := c2structs.C2OPSECMessageResponse{
			Success: true,
			Message: fmt.Sprintf("Called opsec check:\n%v", message),
		}
		return response

	},
	GetIOCFunction: func(message c2structs.C2GetIOCMessage) c2structs.C2GetIOCMessageResponse {
		response := c2structs.C2GetIOCMessageResponse{Success: true}

		return response
	},
	SampleMessageFunction: func(message c2structs.C2SampleMessageMessage) c2structs.C2SampleMessageResponse {
		response := c2structs.C2SampleMessageResponse{Success: true, Message: "\n"}
		agentConfigFileID, err := message.GetFileArg("raw_c2_config")
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %v\n", err)
			return response
		}
		agentConfigContents, err := mythicrpc.SendMythicRPCFileGetContent(mythicrpc.MythicRPCFileGetContentMessage{
			AgentFileID: agentConfigFileID,
		})
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %v\n", err)
			return response
		}
		if !agentConfigContents.Success {
			response.Success = false
			response.Error += fmt.Sprintf("Error getting agent_config: %s\n", agentConfigContents.Error)
			return response
		}
		agentVariation := AgentVariations{}
		err = json.Unmarshal(agentConfigContents.Content, &agentVariation)
		if err != nil {
			err2 := toml.Unmarshal(agentConfigContents.Content, &agentVariation)
			if err2 != nil {
				response.Success = false
				response.Error += fmt.Sprintf("Error parsing agent config: %v\n%v\n", err, err2)
				return response
			}
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
		// get message
		reqGet, err := http.NewRequest(agentVariation.Get.Verb, domains[0]+agentVariation.Get.URI, nil)
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating client get request: %v\n", err)
			return response
		}
		switch agentVariation.Get.Client.Message.Location {
		case "cookie":
			reqGet.AddCookie(&http.Cookie{
				Name:  agentVariation.Get.Client.Message.Name,
				Value: string(agentMessageGetTransformed),
			})
		case "query":
			reqGet.Form.Set(agentVariation.Get.Client.Message.Name, string(agentMessageGetTransformed))
		case "header":
			reqGet.Header.Set(agentVariation.Get.Client.Message.Name, string(agentMessageGetTransformed))
		default:
			// do nothing, it's the body and we already added it
		}
		for key, _ := range agentVariation.Get.Client.Headers {
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
		// adding query parameters is a little weird in go
		q := reqGet.URL.Query()
		for key, _ := range agentVariation.Get.Client.Parameters {
			q.Add(key, agentVariation.Get.Client.Parameters[key])
		}
		if len(agentVariation.Get.Client.Parameters) > 0 {
			reqGet.URL.RawQuery = q.Encode()
		}
		dump, err := httputil.DumpRequest(reqGet, true)
		response.Message += "GET Variation Client Message:\n" + fmt.Sprintf("%s\n\n", dump)
		// get mock server response
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		client := server.Client()
		serverResp, err := client.Get("http://test")
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
		// post message
		var bodyBuffer *bytes.Buffer
		var bodyBytes []byte
		if slices.Contains([]string{"", "body"}, agentVariation.Post.Client.Message.Location) {
			bodyBytes = agentMessagePostTransformed
		} else {
			bodyBytes = make([]byte, 0)
		}
		bodyBuffer = bytes.NewBuffer(bodyBytes)
		reqPost, err := http.NewRequest(agentVariation.Get.Verb, domains[0]+agentVariation.Post.URI, bodyBuffer)
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating client post request: %v\n", err)
			return response
		}
		switch agentVariation.Post.Client.Message.Location {
		case "cookie":
			reqPost.AddCookie(&http.Cookie{
				Name:  agentVariation.Post.Client.Message.Name,
				Value: string(agentMessagePostTransformed),
			})
		case "query":
			reqPost.Form.Set(agentVariation.Post.Client.Message.Name, string(agentMessagePostTransformed))
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
		qPost := reqPost.URL.Query()
		for key, _ := range agentVariation.Post.Client.Parameters {
			qPost.Add(key, agentVariation.Post.Client.Parameters[key])
		}
		if len(agentVariation.Post.Client.Parameters) > 0 {
			reqPost.URL.RawQuery = qPost.Encode()
		}
		dumpPost, err := httputil.DumpRequest(reqPost, true)
		response.Message += "POST Variation Client Message:\n" + fmt.Sprintf("%s\n\n", dumpPost)
		serverResp, err = client.Get("http://test")
		if err != nil {
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating server post response: %v\n\n", err)
			return response
		}
		for key, val := range agentVariation.Post.Server.Headers {
			serverResp.Header.Set(key, val)
		}
		serverResp.Body.Close()
		agentMessage, err = transformMessageFromServer([]byte(samplePostMessage), agentVariation.Post)
		if err != nil {
			logging.LogError(err, "failed to create transformed response for agent")
			response.Success = false
			response.Error += fmt.Sprintf("Error simulating server post response: %v\n", err)
			return response
		}
		serverResp.Body = io.NopCloser(bytes.NewBuffer(agentMessage))
		serverResp.ContentLength = int64(len(agentMessage))
		dump, err = httputil.DumpResponse(serverResp, true)
		response.Message += "POST Variation Server Response:\n" + fmt.Sprintf("%s\n\n", dump)
		return response
	},
	HostFileFunction: func(message c2structs.C2HostFileMessage) c2structs.C2HostFileMessageResponse {
		config, err := getC2JsonConfig()
		if err != nil {
			return c2structs.C2HostFileMessageResponse{
				Success: false,
				Error:   err.Error(),
			}
		}
		for i, _ := range config.Instances {
			if config.Instances[i].PayloadHostPaths == nil {
				config.Instances[i].PayloadHostPaths = make(map[string]string)
			}
			config.Instances[i].PayloadHostPaths[message.HostURL] = message.FileUUID
		}
		err = writeC2JsonConfig(config)
		if err != nil {
			return c2structs.C2HostFileMessageResponse{
				Success: false,
				Error:   err.Error(),
			}
		}
		return c2structs.C2HostFileMessageResponse{
			Success: true,
		}
	},
}
var httpxc2parameters = []c2structs.C2Parameter{
	{
		Name:          "raw_c2_config",
		Description:   "Agent configuration in JSON or TOML file",
		DefaultValue:  "",
		ParameterType: c2structs.C2_PARAMETER_TYPE_FILE,
		Required:      false,
	},
	{
		Name:          "callback_domains",
		Description:   "Array of callback domains to communicate with",
		DefaultValue:  []string{"https://example.com:443"},
		ParameterType: c2structs.C2_PARAMETER_TYPE_ARRAY,
		Required:      true,
	},
	{
		Name:          "domain_rotation",
		Description:   "Domain rotation pattern. Fail-over uses each one in order until it can't communicate with it successfully and moves on. Round-robin makes each request to the next host in the list.",
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Choices: []string{
			"fail-over",
			"round-robin",
		},
	},
	{
		Name:          "failover_threshold",
		Description:   "Domain fail-over threshold for how many times to keep trying one host before moving onto the next",
		DefaultValue:  5,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
	},
	{
		Name:          "encrypted_exchange_check",
		Description:   "Perform Key Exchange",
		DefaultValue:  true,
		ParameterType: c2structs.C2_PARAMETER_TYPE_BOOLEAN,
		Required:      false,
	},
	{
		Name:          "callback_jitter",
		Description:   "Callback Jitter in percent",
		DefaultValue:  23,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
	},
	{
		Name:          "AESPSK",
		Description:   "Encryption Type",
		DefaultValue:  "aes256_hmac",
		ParameterType: c2structs.C2_PARAMETER_TYPE_CHOOSE_ONE,
		Required:      false,
		IsCryptoType:  true,
		Choices: []string{
			"aes256_hmac",
			"none",
		},
	},
	{
		Name:          "callback_interval",
		Description:   "Callback Interval in seconds",
		DefaultValue:  10,
		ParameterType: c2structs.C2_PARAMETER_TYPE_NUMBER,
		Required:      false,
		VerifierRegex: "^[0-9]+$",
	},
	{
		Name:          "killdate",
		Description:   "Date when the agent should stop executing",
		DefaultValue:  365,
		ParameterType: c2structs.C2_PARAMETER_TYPE_DATE,
		Required:      false,
	},
}

func Initialize() {
	c2structs.AllC2Data.Get("httpx").AddC2Definition(httpxc2definition)
	c2structs.AllC2Data.Get("httpx").AddParameters(httpxc2parameters)
}
