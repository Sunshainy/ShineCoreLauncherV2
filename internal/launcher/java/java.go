package java

import (
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

var versionRe = regexp.MustCompile(`version \"([0-9]+)(?:\\.([0-9]+))?`)

func FindSystemJava() string {
	if path, err := exec.LookPath("javaw.exe"); err == nil {
		return path
	}
	if path, err := exec.LookPath("java.exe"); err == nil {
		return path
	}
	if path, err := exec.LookPath("java"); err == nil {
		return path
	}
	return ""
}

func GetJavaMajor(javaPath string) (int, error) {
	if strings.TrimSpace(javaPath) == "" {
		return 0, errors.New("java path is empty")
	}
	// На Windows используем javaw.exe вместо java.exe для скрытия консоли
	path := javaPath
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, "java.exe", "javaw.exe")
		path = strings.ReplaceAll(path, "\\bin\\java.exe", "\\bin\\javaw.exe")
	}
	cmd := exec.Command(path, "-version")
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: 0x08000000, // CREATE_NO_WINDOW
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	return ParseJavaMajor(string(output))
}

func ParseJavaMajor(output string) (int, error) {
	match := versionRe.FindStringSubmatch(output)
	if len(match) < 2 {
		return 0, errors.New("java version not found")
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, err
	}
	// Java 8 sometimes reports "1.8"
	if major == 1 && len(match) > 2 {
		minor, err := strconv.Atoi(match[2])
		if err == nil {
			return minor, nil
		}
	}
	return major, nil
}
