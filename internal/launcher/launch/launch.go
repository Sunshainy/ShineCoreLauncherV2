package launch

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"shinecore/internal/logging"
	"shinecore/internal/launcher/mojang"
)

type LaunchRequest struct {
	BaseDir   string
	Version  string
	Player   PlayerInfo
	JavaPath string
	MemoryMB int
}

type PlayerInfo struct {
	Name string
	UUID string
}

func Launch(ctx context.Context, req LaunchRequest) error {
	resolved, err := resolveVersion(req.BaseDir, req.Version)
	if err != nil {
		return err
	}

	nativesDir := filepath.Join(req.BaseDir, "bin", resolved.ID)
	if err := os.MkdirAll(nativesDir, 0o755); err != nil {
		return err
	}
	if _, err := extractNatives(req.BaseDir, nativesDir, resolved.Libraries); err != nil {
		return err
	}

	classpath := buildClasspath(req.BaseDir, resolved, req.Version)
	if classpath == "" {
		return errors.New("empty classpath")
	}

	args := buildArgs(req, resolved, nativesDir)
	slog.Info("launcher: java start", "java", req.JavaPath, "args_count", len(args))
	cmd := exec.CommandContext(ctx, req.JavaPath, args...)
	cmd.Dir = req.BaseDir
	cmd.Stdout = logging.Writer()
	cmd.Stderr = logging.Writer()
	return cmd.Start()
}

func PrepareNatives(baseDir, version string) (int, error) {
	resolved, err := resolveVersion(baseDir, version)
	if err != nil {
		return 0, err
	}
	nativesDir := filepath.Join(baseDir, "bin", resolved.ID)
	if err := os.MkdirAll(nativesDir, 0o755); err != nil {
		return 0, err
	}
	return extractNatives(baseDir, nativesDir, resolved.Libraries)
}

type resolvedVersion struct {
	ID         string
	MainClass  string
	Libraries  []mojang.Library
	Arguments  mojang.VersionArguments
	AssetIndex mojang.AssetIndex
	ClientVersion string
}

func resolveVersion(baseDir, version string) (*resolvedVersion, error) {
	meta, err := loadVersion(baseDir, version)
	if err != nil {
		return nil, err
	}
	if meta.InheritsFrom == "" {
		return &resolvedVersion{
			ID:         meta.ID,
			MainClass:  meta.MainClass,
			Libraries:  meta.Libraries,
			Arguments:  normalizeArguments(meta),
			AssetIndex: meta.AssetIndex,
			ClientVersion: meta.ID,
		}, nil
	}
	parent, err := resolveVersion(baseDir, meta.InheritsFrom)
	if err != nil {
		return nil, err
	}
	merged := &resolvedVersion{
		ID:         meta.ID,
		MainClass:  meta.MainClass,
		Libraries:  append(parent.Libraries, meta.Libraries...),
		Arguments:  mergeArguments(parent.Arguments, normalizeArguments(meta)),
		AssetIndex: meta.AssetIndex,
		ClientVersion: parent.ClientVersion,
	}
	if merged.MainClass == "" {
		merged.MainClass = parent.MainClass
	}
	if merged.AssetIndex.ID == "" {
		merged.AssetIndex = parent.AssetIndex
	}
	return merged, nil
}

func loadVersion(baseDir, version string) (*mojang.VersionMetadata, error) {
	path := filepath.Join(baseDir, "versions", version, version+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta mojang.VersionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func normalizeArguments(meta *mojang.VersionMetadata) mojang.VersionArguments {
	if len(meta.Arguments.Game) > 0 || len(meta.Arguments.Jvm) > 0 {
		return meta.Arguments
	}
	if strings.TrimSpace(meta.MinecraftArguments) == "" {
		return mojang.VersionArguments{}
	}
	args := strings.Fields(meta.MinecraftArguments)
	out := make([]mojang.Argument, 0, len(args))
	for _, arg := range args {
		out = append(out, mojang.Argument{Value: arg})
	}
	return mojang.VersionArguments{Game: out}
}

func mergeArguments(parent, child mojang.VersionArguments) mojang.VersionArguments {
	return mojang.VersionArguments{
		Game: append(parent.Game, child.Game...),
		Jvm:  append(parent.Jvm, child.Jvm...),
	}
}

func buildClasspath(baseDir string, resolved *resolvedVersion, rootVersion string) string {
	entries := make([]string, 0, len(resolved.Libraries)+1)
	for _, lib := range resolved.Libraries {
		if !mojangAllowLibrary(lib.Rules) {
			continue
		}
		path := libraryArtifactPath(baseDir, lib)
		if path != "" {
			entries = append(entries, path)
		}
	}
	clientVersion := rootVersion
	if resolved.ClientVersion != "" {
		clientVersion = resolved.ClientVersion
	}
	clientJar := filepath.Join(baseDir, "versions", clientVersion, clientVersion+".jar")
	entries = append(entries, clientJar)
	return strings.Join(entries, string(os.PathListSeparator))
}

func buildArgs(req LaunchRequest, resolved *resolvedVersion, nativesDir string) []string {
	var args []string
	args = append(args, buildMemoryArgs(req.MemoryMB)...)
	for _, arg := range resolved.Arguments.Jvm {
		for _, v := range expandArgument(arg) {
			args = append(args, replaceVars(v, req, resolved, nativesDir))
		}
	}
	args = append(args, "-Djava.library.path="+nativesDir)
	args = append(args, "-cp", buildClasspath(req.BaseDir, resolved, req.Version))
	args = append(args, resolved.MainClass)
	skipNext := false
	for _, arg := range resolved.Arguments.Game {
		for _, v := range expandArgument(arg) {
			if skipNext {
				skipNext = false
				continue
			}
			if shouldSkipArgument(v) {
				skipNext = strings.HasPrefix(v, "--quickPlay")
				continue
			}
			args = append(args, replaceVars(v, req, resolved, nativesDir))
		}
	}
	return args
}

func buildMemoryArgs(memoryMB int) []string {
	if memoryMB <= 0 {
		return nil
	}
	xms := 512
	if memoryMB < xms {
		xms = memoryMB
	}
	return []string{
		"-Xms" + strconv.Itoa(xms) + "m",
		"-Xmx" + strconv.Itoa(memoryMB) + "m",
	}
}

func shouldSkipArgument(arg string) bool {
	if strings.HasPrefix(arg, "--quickPlay") {
		return true
	}
	if arg == "--demo" {
		return true
	}
	return false
}

func expandArgument(arg mojang.Argument) []string {
	if arg.Value == nil {
		return nil
	}
	if !mojangAllowLibrary(arg.Rules) {
		return nil
	}
	switch v := arg.Value.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func replaceVars(input string, req LaunchRequest, resolved *resolvedVersion, nativesDir string) string {
	replacements := map[string]string{
		"${auth_player_name}": req.Player.Name,
		"${auth_access_token}": "0",
		"${clientid}":          "",
		"${version_name}":     resolved.ID,
		"${game_directory}":   req.BaseDir,
		"${assets_root}":      filepath.Join(req.BaseDir, "assets"),
		"${assets_index_name}": resolved.AssetIndex.ID,
		"${auth_uuid}":        req.Player.UUID,
		"${auth_xuid}":        "0",
		"${user_type}":        "offline",
		"${user_properties}": "{}",
		"${version_type}":     "release",
		"${natives_directory}": nativesDir,
		"${classpath}":        buildClasspath(req.BaseDir, resolved, req.Version),
		"${resolution_width}":  "854",
		"${resolution_height}": "480",
	}
	for key, value := range replacements {
		input = strings.ReplaceAll(input, key, value)
	}
	return input
}

func extractNatives(baseDir, nativesDir string, libraries []mojang.Library) (int, error) {
	count := 0
	for _, lib := range libraries {
		if len(lib.Natives) == 0 || lib.Downloads == nil || len(lib.Downloads.Classifiers) == 0 {
			continue
		}
		classifier := nativeClassifier(lib)
		if classifier == "" {
			continue
		}
		native, ok := lib.Downloads.Classifiers[classifier]
		if !ok {
			continue
		}
		count++
		jarPath := filepath.Join(baseDir, "libraries", filepath.FromSlash(native.Path))
		if err := extractNativeJar(jarPath, nativesDir, lib.Extract); err != nil {
			return count, err
		}
	}
	return count, nil
}

func extractNativeJar(path string, dst string, extract *mojang.LibraryExtract) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	exclude := map[string]struct{}{}
	if extract != nil {
		for _, item := range extract.Exclude {
			exclude[item] = struct{}{}
		}
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.HasPrefix(file.Name, "META-INF/") {
			continue
		}
		if _, found := exclude[file.Name]; found {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(file.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			in.Close()
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			in.Close()
			out.Close()
			return err
		}
		in.Close()
		out.Close()
	}
	return nil
}

func nativeClassifier(lib mojang.Library) string {
	key := "windows"
	if runtime.GOOS == "linux" {
		key = "linux"
	} else if runtime.GOOS == "darwin" {
		key = "osx"
	}
	if value, ok := lib.Natives[key]; ok {
		return strings.ReplaceAll(value, "${arch}", "64")
	}
	return ""
}

func libraryArtifactPath(baseDir string, lib mojang.Library) string {
	if lib.Downloads != nil && lib.Downloads.Artifact != nil {
		return filepath.Join(baseDir, "libraries", filepath.FromSlash(lib.Downloads.Artifact.Path))
	}
	if lib.Name == "" {
		return ""
	}
	path := mojangLibraryPath(lib.Name)
	return filepath.Join(baseDir, "libraries", filepath.FromSlash(path))
}

func mojangLibraryPath(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) < 3 {
		return ""
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact := parts[1]
	version := parts[2]
	classifier := ""
	ext := "jar"
	if len(parts) >= 4 {
		classifier = parts[3]
	}
	if strings.Contains(version, "@") {
		split := strings.Split(version, "@")
		version = split[0]
		ext = split[1]
	}
	file := artifact + "-" + version
	if classifier != "" {
		file += "-" + classifier
	}
	file += "." + ext
	return group + "/" + artifact + "/" + version + "/" + file
}

func mojangAllowLibrary(rules []mojang.Rule) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range rules {
		match := true
		if rule.OS != nil && rule.OS.Name != "" {
			match = rule.OS.Name == osName()
		}
		if rule.Action == "allow" && match {
			allowed = true
		}
		if rule.Action == "disallow" && match {
			allowed = false
		}
	}
	return allowed
}

func osName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	default:
		return "linux"
	}
}
