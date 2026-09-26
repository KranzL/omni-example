package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

const (
	AnthropicAPIKeyName = "ANTHROPIC_API_KEY"
	VeniceAPIKeyName    = "VENICE_API_KEY"
	JevAPIKeyName       = "JEV_API_KEY"
	DatabaseURLName     = "DATABASE_URL"
	AnthropicWorkspace  = "ANTHROPIC_WORKSPACE_ID"
	EnvFileOverride     = "OMNI_ENV_FILE"
	LLMProviderName     = "LLM_PROVIDER"
	VeniceModelCheap    = "VENICE_MODEL_CHEAP"
	VeniceModelMid      = "VENICE_MODEL_MID"
	VeniceModelTop      = "VENICE_MODEL_TOP"
	EnvFileName         = ".env"
)

type Config struct {
	AnthropicAPIKey      string
	VeniceAPIKey         string
	JevAPIKey            string
	DatabaseURL          string
	AnthropicWorkspaceID string
	LLMProvider          string
	VeniceModels         map[string]string
}

func FindEnvFile() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return FindEnvFileIn(dir)
}

func FindEnvFileIn(startDir string) string {
	candidate := filepath.Join(startDir, EnvFileName)
	if fileExists(candidate) {
		return candidate
	}
	if override := os.Getenv(EnvFileOverride); override != "" {
		if resolved := resolveOverride(override); resolved != "" {
			return resolved
		}
	}
	return mainCheckoutEnv(startDir)
}

func resolveOverride(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		candidate := filepath.Join(path, EnvFileName)
		if fileExists(candidate) {
			return candidate
		}
		return ""
	}
	return path
}

func mainCheckoutEnv(startDir string) string {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = startDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	commonDir := strings.TrimSpace(string(out))
	if commonDir == "" {
		return ""
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(startDir, commonDir)
	}
	candidate := filepath.Join(filepath.Dir(commonDir), EnvFileName)
	if fileExists(candidate) {
		return candidate
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func Load() (Config, error) {
	dir, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	return LoadIn(dir)
}

func LoadIn(startDir string) (Config, error) {
	if path := FindEnvFileIn(startDir); path != "" {
		if err := godotenv.Load(path); err != nil {
			return Config{}, err
		}
	}
	return Config{
		AnthropicAPIKey:      os.Getenv(AnthropicAPIKeyName),
		VeniceAPIKey:         os.Getenv(VeniceAPIKeyName),
		JevAPIKey:            os.Getenv(JevAPIKeyName),
		DatabaseURL:          os.Getenv(DatabaseURLName),
		AnthropicWorkspaceID: os.Getenv(AnthropicWorkspace),
		LLMProvider:          os.Getenv(LLMProviderName),
		VeniceModels: map[string]string{
			"cheap": os.Getenv(VeniceModelCheap),
			"mid":   os.Getenv(VeniceModelMid),
			"top":   os.Getenv(VeniceModelTop),
		},
	}, nil
}

func (c Config) Missing() []string {
	var missing []string
	if c.AnthropicAPIKey == "" {
		missing = append(missing, AnthropicAPIKeyName)
	}
	if c.VeniceAPIKey == "" {
		missing = append(missing, VeniceAPIKeyName)
	}
	if c.DatabaseURL == "" {
		missing = append(missing, DatabaseURLName)
	}
	return missing
}
