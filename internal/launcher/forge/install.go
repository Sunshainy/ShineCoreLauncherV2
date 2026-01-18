package forge

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"shinecore/internal/launcher/download"
	"shinecore/internal/launcher/mojang"
)

type LoaderKind string

const (
	LoaderForge    LoaderKind = "forge"
	LoaderNeoForge LoaderKind = "neoforge"
)

type InstallRequest struct {
	BaseDir      string
	GameVersion  string
	LoaderKind   LoaderKind
	LoaderVersion string
	JavaPath     string
	Client       *http.Client
}

func EnsureInstalled(ctx context.Context, req InstallRequest) (string, error) {
	if strings.TrimSpace(req.LoaderVersion) == "" {
		return "", errors.New("loader version required for forge/neoforge")
	}
	if strings.TrimSpace(req.JavaPath) == "" {
		return "", errors.New("java path required for forge/neoforge install")
	}
	client := req.Client
	if client == nil {
		client = http.DefaultClient
	}

	installerURLs := installerURLs(req.LoaderKind, req.LoaderVersion)
	if len(installerURLs) == 0 {
		return "", errors.New("unsupported loader")
	}
	installerPath := filepath.Join(req.BaseDir, "installers", string(req.LoaderKind)+"-"+req.LoaderVersion+"-installer.jar")
	
	// Пытаемся загрузить установщик с основного URL, при неудаче - с резервных зеркал
	var lastErr error
	for i, url := range installerURLs {
		err := download.EnsureFile(ctx, client, url, installerPath, 0, "", nil)
		if err == nil {
			if i > 0 {
				log.Printf("forge: installer downloaded from fallback mirror %d/%d", i+1, len(installerURLs))
			}
			break // Успешно загрузили
		}
		lastErr = err
		if i < len(installerURLs)-1 {
			log.Printf("forge: installer download failed from mirror %d/%d, trying next...", i+1, len(installerURLs))
		}
	}
	
	if lastErr != nil {
		return "", fmt.Errorf("forge installer download failed from all mirrors: %w", lastErr)
	}

	// Ensure base game.
	if _, err := mojang.EnsureInstalled(ctx, mojang.InstallRequest{
		BaseDir: req.BaseDir,
		Version: req.GameVersion,
		Client:  client,
	}); err != nil {
		return "", err
	}

	profile, versionMeta, err := readInstallerProfile(installerPath)
	if err != nil {
		return "", err
	}

	tmpDir := filepath.Join(req.BaseDir, "tmp", "forge-"+req.LoaderVersion)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", err
	}

	librariesDir := filepath.Join(req.BaseDir, "libraries")
	if err := os.MkdirAll(librariesDir, 0o755); err != nil {
		return "", err
	}

	libraries := map[string]string{}
	if err := downloadInstallerLibraries(ctx, client, installerPath, librariesDir, profile, libraries); err != nil {
		return "", err
	}

	data, err := buildInstallData(installerPath, tmpDir, librariesDir, profile, req.GameVersion, req.BaseDir, req.LoaderVersion)
	if err != nil {
		return "", err
	}

	if err := runProcessors(ctx, req.JavaPath, librariesDir, profile, libraries, data); err != nil {
		return "", err
	}

	versionID := buildVersionID(req.LoaderKind, req.LoaderVersion)
	versionMeta.ID = versionID
	if versionMeta.InheritsFrom == "" {
		versionMeta.InheritsFrom = req.GameVersion
	}
	versionDir := filepath.Join(req.BaseDir, "versions", versionID)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", err
	}
	metaPath := filepath.Join(versionDir, versionID+".json")
	payload, err := json.MarshalIndent(versionMeta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(metaPath, payload, 0o644); err != nil {
		return "", err
	}

	return versionID, nil
}

func installerURL(kind LoaderKind, version string) string {
	switch kind {
	case LoaderForge:
		return fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", version, version)
	case LoaderNeoForge:
		return fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", version, version)
	default:
		return ""
	}
}

// installerURLs возвращает список URL для загрузчика с резервными зеркалами
func installerURLs(kind LoaderKind, version string) []string {
	switch kind {
	case LoaderForge:
		return []string{
			// Основной официальный Maven репозиторий
			fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", version, version),
			// Официальное зеркало files.minecraftforge.net
			fmt.Sprintf("https://files.minecraftforge.net/maven/net/minecraftforge/forge/%s/forge-%s-installer.jar", version, version),
			// FastMinecraftMirror - быстрое зеркало
			fmt.Sprintf("https://forge.fastmcmirror.org/net/minecraftforge/forge/%s/forge-%s-installer.jar", version, version),
		}
	case LoaderNeoForge:
		return []string{
			// Основной официальный репозиторий (releases)
			fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", version, version),
			// Альтернативный путь (без releases)
			fmt.Sprintf("https://maven.neoforged.net/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", version, version),
		}
	default:
		return []string{}
	}
}

func buildVersionID(kind LoaderKind, version string) string {
	return fmt.Sprintf("%s-%s", kind, version)
}

func readInstallerProfile(installerPath string) (*InstallProfile, *mojang.VersionMetadata, error) {
	reader, err := zip.OpenReader(installerPath)
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()

	var profileData []byte
	var versionData []byte
	for _, file := range reader.File {
		switch file.Name {
		case "install_profile.json":
			profileData, err = readZipFile(file)
			if err != nil {
				return nil, nil, err
			}
		case "version.json":
			versionData, err = readZipFile(file)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if profileData == nil {
		return nil, nil, errors.New("install_profile.json not found")
	}
	var profile InstallProfile
	if err := json.Unmarshal(profileData, &profile); err != nil {
		return nil, nil, err
	}
	if versionData == nil && profile.JSON != "" {
		for _, file := range reader.File {
			if file.Name == profile.JSON {
				versionData, err = readZipFile(file)
				if err != nil {
					return nil, nil, err
				}
				break
			}
		}
	}
	if versionData == nil {
		return nil, nil, errors.New("version.json not found in installer")
	}
	var meta mojang.VersionMetadata
	if err := json.Unmarshal(versionData, &meta); err != nil {
		return nil, nil, err
	}
	return &profile, &meta, nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	in, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer in.Close()
	return io.ReadAll(in)
}

func downloadInstallerLibraries(ctx context.Context, client *http.Client, installerPath, librariesDir string, profile *InstallProfile, libraries map[string]string) error {
	reader, err := zip.OpenReader(installerPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, lib := range profile.Libraries {
		path := lib.Downloads.Artifact.Path
		dst := filepath.Join(librariesDir, filepath.FromSlash(path))
		libraries[lib.Name.String()] = dst
		if lib.Downloads.Artifact.URL != "" {
			if err := download.EnsureFile(ctx, client, lib.Downloads.Artifact.URL, dst, lib.Downloads.Artifact.Size, "", nil); err != nil {
				return err
			}
			continue
		}
		if err := extractMavenArtifact(reader, lib.Name, dst); err != nil {
			return err
		}
	}
	return nil
}

func extractMavenArtifact(reader *zip.ReadCloser, gav GAV, dst string) error {
	want := "maven/" + gav.FilePath()
	for _, file := range reader.File {
		if file.Name == want {
			data, err := readZipFile(file)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dst, data, 0o644)
		}
	}
	return fmt.Errorf("installer artifact missing: %s", gav.String())
}

type dataValue struct {
	Kind  string
	Value string
}

func buildInstallData(installerPath, tmpDir, librariesDir string, profile *InstallProfile, gameVersion, baseDir, loaderVersion string) (map[string]dataValue, error) {
	reader, err := zip.OpenReader(installerPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data := map[string]dataValue{}
	for name, entry := range profile.Data {
		raw := entry.ForSide("client")
		switch {
		case strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]"):
			gav, err := ParseGAV(raw[1 : len(raw)-1])
			if err != nil {
				return nil, err
			}
			data[name] = dataValue{Kind: "library", Value: gav.String()}
		case strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'"):
			data[name] = dataValue{Kind: "literal", Value: raw[1 : len(raw)-1]}
		default:
			filePath := strings.TrimPrefix(raw, "/")
			tmpFile := filepath.Join(tmpDir, filepath.FromSlash(filePath))
			if err := extractInstallerFile(reader, filePath, tmpFile); err != nil {
				return nil, err
			}
			data[name] = dataValue{Kind: "file", Value: tmpFile}
		}
	}
	clientJar := filepath.Join(baseDir, "versions", gameVersion, gameVersion+".jar")
	data["SIDE"] = dataValue{Kind: "literal", Value: "client"}
	data["MINECRAFT_JAR"] = dataValue{Kind: "file", Value: clientJar}
	data["MINECRAFT_VERSION"] = dataValue{Kind: "literal", Value: gameVersion}
	data["INSTALLER"] = dataValue{Kind: "file", Value: installerPath}
	data["LIBRARY_DIR"] = dataValue{Kind: "file", Value: librariesDir}
	data["LOADER_VERSION"] = dataValue{Kind: "literal", Value: loaderVersion}
	return data, nil
}

func extractInstallerFile(reader *zip.ReadCloser, entry, dst string) error {
	for _, file := range reader.File {
		if file.Name == entry || file.Name == "/"+entry {
			data, err := readZipFile(file)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dst, data, 0o644)
		}
	}
	return fmt.Errorf("installer file missing: %s", entry)
}

func runProcessors(ctx context.Context, javaPath, librariesDir string, profile *InstallProfile, libraries map[string]string, data map[string]dataValue) error {
	for _, proc := range profile.Processors {
		if len(proc.Sides) > 0 && !contains(proc.Sides, "client") {
			continue
		}
		jarPath := libraries[proc.Jar.String()]
		if jarPath == "" {
			return fmt.Errorf("processor jar missing: %s", proc.Jar.String())
		}
		mainClass, err := findMainClass(jarPath)
		if err != nil {
			return err
		}
		classpath := []string{jarPath}
		for _, dep := range proc.Classpath {
			depPath := libraries[dep.String()]
			if depPath == "" {
				return fmt.Errorf("processor classpath missing: %s", dep.String())
			}
			classpath = append(classpath, depPath)
		}
		args := []string{"-cp", strings.Join(classpath, string(os.PathListSeparator)), mainClass}
		for _, arg := range proc.Args {
			formatted := formatProcessorArg(arg, librariesDir, data)
			if formatted != "" {
				args = append(args, formatted)
			}
		}
		path := javaPath
		if runtime.GOOS == "windows" {
			path = strings.ReplaceAll(path, "java.exe", "javaw.exe")
			path = strings.ReplaceAll(path, "\\bin\\java.exe", "\\bin\\javaw.exe")
		}
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir = librariesDir
		if runtime.GOOS == "windows" {
			cmd.SysProcAttr = &syscall.SysProcAttr{
				HideWindow:    true,
				CreationFlags: 0x08000000, // CREATE_NO_WINDOW
			}
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("processor failed: %s: %w", string(out), err)
		}
	}
	return nil
}

func findMainClass(jarPath string) (string, error) {
	reader, err := zip.OpenReader(jarPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if strings.EqualFold(file.Name, "META-INF/MANIFEST.MF") {
			data, err := readZipFile(file)
			if err != nil {
				return "", err
			}
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Main-Class:") {
					return strings.TrimSpace(strings.TrimPrefix(line, "Main-Class:")), nil
				}
			}
		}
	}
	return "", errors.New("main class not found in jar")
}

func formatProcessorArg(input string, librariesDir string, data map[string]dataValue) string {
	if strings.HasPrefix(input, "[") && strings.HasSuffix(input, "]") {
		gav, err := ParseGAV(input[1 : len(input)-1])
		if err == nil {
			return filepath.Join(librariesDir, filepath.FromSlash(gav.FilePath()))
		}
	}
	var out strings.Builder
	token := strings.Builder{}
	inToken := false
	escaped := false
	for _, ch := range input {
		switch {
		case ch == '\\' && !escaped:
			escaped = true
		case ch == '{' && !escaped && !inToken:
			inToken = true
			token.Reset()
		case ch == '}' && !escaped && inToken:
			inToken = false
			val, ok := data[token.String()]
			if ok {
				switch val.Kind {
				case "library":
					gav, err := ParseGAV(val.Value)
					if err == nil {
						out.WriteString(filepath.Join(librariesDir, filepath.FromSlash(gav.FilePath())))
					}
				default:
					out.WriteString(val.Value)
				}
			}
		default:
			if inToken {
				token.WriteRune(ch)
			} else {
				out.WriteRune(ch)
			}
			escaped = false
		}
	}
	return out.String()
}

func contains(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
