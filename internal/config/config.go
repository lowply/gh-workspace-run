package config

import (
	"errors"
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
	path, err := Path()
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

func Lookup(repository string) (string, bool, error) {
	path, err := Path()
	if err != nil {
		return "", false, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read configuration %s: %w", path, err)
	}
	config, err := parse(path, content)
	if err != nil {
		return "", false, err
	}
	codespace := config.Repositories[repository]
	return codespace, codespace != "", nil
}

func Set(repository, codespace string) (string, error) {
	if repository == "" {
		return "", fmt.Errorf("repository must not be empty")
	}
	if codespace == "" {
		return "", fmt.Errorf("Codespace must not be empty")
	}
	path, err := Path()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create configuration directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure configuration directory: %w", err)
	}

	config := fileConfig{}
	content, err := os.ReadFile(path)
	if err == nil {
		config, err = parse(path, content)
		if err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read configuration %s: %w", path, err)
	}
	if config.Repositories == nil {
		config.Repositories = make(map[string]string)
	}
	config.Repositories[repository] = codespace
	content, err = yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("serialize configuration %s: %w", path, err)
	}
	if err := write(path, content); err != nil {
		return "", err
	}
	return path, nil
}

func parse(path string, content []byte) (fileConfig, error) {
	var config fileConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return fileConfig{}, fmt.Errorf("parse configuration %s: %w", path, err)
	}
	return config, nil
}

func write(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".config.yml-*")
	if err != nil {
		return fmt.Errorf("create temporary configuration for %s: %w", path, err)
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("secure temporary configuration for %s: %w", path, err)
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return fmt.Errorf("write temporary configuration for %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary configuration for %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace configuration %s: %w", path, err)
	}
	return nil
}

func Path() (string, error) {
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
