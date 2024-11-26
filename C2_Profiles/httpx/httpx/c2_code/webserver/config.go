package webserver

import (
	"encoding/json"
	"github.com/MythicMeta/MythicContainer/logging"
	"log"
	"os"
	"path/filepath"
)

type config struct {
	Instances []instanceConfig `json:"instances"`
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
	URIs   []string                   `json:"uris" toml:"uris"`
	Client AgentVariationConfigClient `json:"client" toml:"client"`
	Server AgentVariationConfigServer `json:"server" toml:"server"`
}
type AgentVariations struct {
	Name string               `json:"name" toml:"name"`
	Get  AgentVariationConfig `json:"get" toml:"get"`
	Post AgentVariationConfig `json:"post" toml:"post"`
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

var (
	Config          = config{}
	AgentConfigs    map[string]AgentVariations
	configPath      = "config.json"
	agentConfigPath = "agent_configs.json"
)

func InitializeLocalConfig() error {
	if !fileExists(filepath.Join(getCwdFromExe(), configPath)) {
		_, err := os.Create(filepath.Join(getCwdFromExe(), configPath))
		if err != nil {
			logging.LogError(err, "[-] config.json doesn't exist and couldn't be created")
			return err
		}
	}
	fileData, err := os.ReadFile(configPath)
	if err != nil {
		logging.LogError(err, "Failed to read in config.json file")
		return err
	}
	err = json.Unmarshal(fileData, &Config)
	if err != nil {
		logging.LogError(err, "Failed to unmarshal config bytes")
		return err
	}
	logging.LogInfo("[+] Successfully read in config.json")
	return nil
}
func InitializeLocalAgentConfig() error {
	if !fileExists(filepath.Join(getCwdFromExe(), agentConfigPath)) {
		_, err := os.Create(filepath.Join(getCwdFromExe(), agentConfigPath))
		if err != nil {
			logging.LogError(err, "[-] agent_configs.json doesn't exist and couldn't be created")
			return err
		}
	}
	fileData, err := os.ReadFile(agentConfigPath)
	if err != nil {
		logging.LogError(err, "Failed to read in agent_configs.json file")
		return err
	}
	err = json.Unmarshal(fileData, &AgentConfigs)
	if err != nil {
		logging.LogError(err, "Failed to unmarshal config bytes")
		return err
	}
	logging.LogInfo("[+] Successfully read in agent_configs.json")
	return nil
}

func getCwdFromExe() string {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("[-] Failed to get path to current executable: %v", err)
	}
	return filepath.Dir(exe)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return !info.IsDir()
}
