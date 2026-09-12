package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type fileConfig struct {
	Repositories map[string]string `yaml:"repositories"`
}

const template = "repositories:\n  owner/repository: codespace-name\n"

func Ensure() (string, bool, error) {
	path, err := configPath()
	if err != nil {
		return "", false, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, fmt.Errorf("create configuration directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", false, fmt.Errorf("secure configuration directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", false, fmt.Errorf("inspect existing configuration: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			return "", false, fmt.Errorf("configuration path is not a regular file: %s", path)
		}
		return path, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("create configuration %s: %w", path, err)
	}
	if _, err := file.WriteString(template); err != nil {
		file.Close()
		os.Remove(path)
		return "", false, fmt.Errorf("write configuration %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", false, fmt.Errorf("close configuration %s: %w", path, err)
	}
	return path, true, nil
}

func Codespace(repository string) (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read configuration %s: %w", path, err)
	}
	var config fileConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return "", fmt.Errorf("parse configuration %s: %w", path, err)
	}
	codespace := config.Repositories[repository]
	if codespace == "" {
		return "", fmt.Errorf("repository %s is not mapped in %s", repository, path)
	}
	return codespace, nil
}

func configPath() (string, error) {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "gh-workspace-run", "config.yml"), nil
}
