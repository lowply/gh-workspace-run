package connection

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lowply/gh-workspace-run/internal/process"
)

type Config struct {
	Path string
	Host string
}

func Prepare(
	ctx context.Context,
	runner process.Runner,
	codespace string,
	persist time.Duration,
) (Config, error) {
	cacheDir, err := cacheDirectory()
	if err != nil {
		return Config{}, err
	}

	configPath := cachePath(cacheDir, codespace)
	if cached, err := os.ReadFile(configPath); err == nil {
		if host, err := concreteHost(string(cached)); err == nil {
			config := Config{Path: configPath, Host: host}
			if active, _ := recoverStaleSocket(ctx, runner, config); active {
				return config, nil
			}
		}
	} else if !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("read cached SSH config: %w", err)
	}

	generated, err := runner.Output(ctx, "", "gh", "codespace", "ssh", "-c", codespace, "--config")
	if err != nil {
		return Config{}, fmt.Errorf("generate Codespaces SSH config: %w", err)
	}
	host, err := concreteHost(string(generated))
	if err != nil {
		return Config{}, err
	}

	controlPath := filepath.Join(cacheDir, "cm-%C")
	controlPath = socketPath(cacheDir, codespace)
	content := fmt.Sprintf(
		"Host %s\n\tControlMaster auto\n\tControlPersist %s\n\tControlPath %s\n\n%s",
		host,
		persist,
		sshConfigValue(controlPath),
		generated,
	)
	if err := writeAtomic(configPath, []byte(content)); err != nil {
		return Config{}, fmt.Errorf("cache SSH config: %w", err)
	}

	config := Config{Path: configPath, Host: host}
	if _, err := recoverStaleSocket(ctx, runner, config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func cacheDirectory() (string, error) {
	cacheDir, err := cacheDirectoryPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("create SSH config cache: %w", err)
	}
	if err := os.Chmod(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("secure SSH config cache: %w", err)
	}
	return cacheDir, nil
}

func cacheDirectoryPath() (string, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}
	return filepath.Join(cacheRoot, "gh-workspace-run"), nil
}

func cachePath(cacheDir, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(cacheDir, fmt.Sprintf("config-%x", sum[:8]))
}

func socketPath(cacheDir, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(cacheDir, fmt.Sprintf("cm-%x", sum[:8]))
}

func Cleanup(maxAge ...time.Duration) (string, []string, error) {
	if len(maxAge) > 1 {
		return "", nil, fmt.Errorf("cleanup accepts at most one maximum age")
	}
	if len(maxAge) == 1 && maxAge[0] < 0 {
		return "", nil, fmt.Errorf("cleanup maximum age must not be negative")
	}
	cacheDir, err := cacheDirectoryPath()
	if err != nil {
		return "", nil, err
	}
	entries, err := os.ReadDir(cacheDir)
	if os.IsNotExist(err) {
		return cacheDir, nil, nil
	}
	if err != nil {
		return cacheDir, nil, fmt.Errorf("read SSH cache: %w", err)
	}
	purgeAll := len(maxAge) == 0
	var cutoff time.Time
	if !purgeAll {
		cutoff = time.Now().Add(-maxAge[0])
	}
	var removed []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "config-") &&
			!strings.HasPrefix(entry.Name(), "cm-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return cacheDir, removed, fmt.Errorf("inspect SSH cache entry: %w", err)
		}
		if !purgeAll && !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(cacheDir, entry.Name())); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return cacheDir, removed, fmt.Errorf("remove SSH cache entry: %w", err)
		}
		removed = append(removed, entry.Name())
	}
	return cacheDir, removed, nil
}

func concreteHost(config string) (string, error) {
	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Host") {
			continue
		}
		for _, host := range fields[1:] {
			if !strings.ContainsAny(host, "*?!") {
				return host, nil
			}
		}
	}
	return "", fmt.Errorf("generated SSH config has no concrete Host")
}

func writeAtomic(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func recoverStaleSocket(ctx context.Context, runner process.Runner, config Config) (bool, error) {
	output, err := runner.Output(ctx, "", "ssh", "-G", "-F", config.Path, config.Host)
	if err != nil {
		return false, fmt.Errorf("resolve SSH control path: %w", err)
	}
	controlPath := configValue(string(output), "controlpath")
	if controlPath == "" {
		return false, nil
	}
	if _, err := runner.Output(ctx, "", "ssh", "-O", "check", "-F", config.Path, config.Host); err == nil {
		return true, nil
	}
	if _, err := os.Lstat(controlPath); err == nil {
		if err := os.Remove(controlPath); err != nil {
			return false, fmt.Errorf("remove stale SSH control socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect SSH control socket: %w", err)
	}
	return false, nil
}

func configValue(config, key string) string {
	for _, line := range strings.Split(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[0], key) {
			return fields[1]
		}
	}
	return ""
}

func sshConfigValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
