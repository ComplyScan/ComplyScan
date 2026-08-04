package installer_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellInstallerVerifiesAndInstallsReleaseArchive(t *testing.T) {
	requireInstallerCommands(t)
	archiveName := installerArchiveName(t, "1.2.3")
	archive := releaseArchive(t, "#!/bin/sh\nprintf 'installed fixture\\n'\n")
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName)
	releaseBase := releaseFixture(t, "v1.2.3", archiveName, archive, checksum)

	installDirectory := t.TempDir()
	command := exec.Command("/bin/sh", filepath.Join("..", "..", "install.sh"),
		"--version", "v1.2.3", "--install-dir", installDirectory, "--no-setup")
	command.Env = append(os.Environ(), "COMPLYSCAN_RELEASE_BASE_URL="+releaseBase)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("installer failed: %v\n%s", err, output)
	}
	installed := filepath.Join(installDirectory, "complyscan")
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed binary mode = %o", info.Mode().Perm())
	}
	if !strings.Contains(string(output), "Verified SHA-256 checksum") {
		t.Fatalf("verification was not reported:\n%s", output)
	}
}

func TestShellInstallerRejectsChecksumMismatch(t *testing.T) {
	requireInstallerCommands(t)
	archiveName := installerArchiveName(t, "1.2.3")
	archive := releaseArchive(t, "fixture")
	releaseBase := releaseFixture(t, "v1.2.3", archiveName, archive, strings.Repeat("0", 64)+"  "+archiveName+"\n")

	installDirectory := filepath.Join(t.TempDir(), "bin")
	command := exec.Command("/bin/sh", filepath.Join("..", "..", "install.sh"),
		"--version", "1.2.3", "--install-dir", installDirectory, "--no-setup")
	command.Env = append(os.Environ(), "COMPLYSCAN_RELEASE_BASE_URL="+releaseBase)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("installer accepted a mismatched checksum:\n%s", output)
	}
	if !strings.Contains(string(output), "checksum verification failed") {
		t.Fatalf("unexpected failure:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(installDirectory, "complyscan")); !os.IsNotExist(statErr) {
		t.Fatalf("binary exists after failed verification: %v", statErr)
	}
}

func TestShellInstallerPreservesExistingBinaryWhenReleaseIsMissing(t *testing.T) {
	requireInstallerCommands(t)
	installDirectory := t.TempDir()
	installed := filepath.Join(installDirectory, "complyscan")
	if err := os.WriteFile(installed, []byte("existing binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("/bin/sh", filepath.Join("..", "..", "install.sh"),
		"--version", "v1.2.3", "--install-dir", installDirectory, "--no-setup")
	command.Env = append(os.Environ(), "COMPLYSCAN_RELEASE_BASE_URL="+fileURL(t.TempDir()))
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("installer accepted a missing release:\n%s", output)
	}
	content, readErr := os.ReadFile(installed)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "existing binary\n" {
		t.Fatalf("existing installation changed after failed update: %q", content)
	}
}

func TestShellInstallerRejectsChecksumWithoutPlatformArchive(t *testing.T) {
	requireInstallerCommands(t)
	archiveName := installerArchiveName(t, "1.2.3")
	archive := releaseArchive(t, "fixture")
	releaseBase := releaseFixture(t, "v1.2.3", archiveName, archive, strings.Repeat("a", 64)+"  another-archive.tar.gz\n")

	installDirectory := t.TempDir()
	command := exec.Command("/bin/sh", filepath.Join("..", "..", "install.sh"),
		"--version", "v1.2.3", "--install-dir", installDirectory, "--no-setup")
	command.Env = append(os.Environ(), "COMPLYSCAN_RELEASE_BASE_URL="+releaseBase)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("installer accepted an incomplete checksum manifest:\n%s", output)
	}
	if !strings.Contains(string(output), "does not contain "+archiveName) {
		t.Fatalf("unexpected failure:\n%s", output)
	}
}

func TestShellInstallerRejectsUnsupportedOperatingSystemBeforeDownload(t *testing.T) {
	requireInstallerCommands(t)
	commandDirectory := t.TempDir()
	uname := filepath.Join(commandDirectory, "uname")
	if err := os.WriteFile(uname, []byte("#!/bin/sh\nprintf 'Plan9\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("/bin/sh", filepath.Join("..", "..", "install.sh"), "--no-setup")
	command.Env = append(os.Environ(), "PATH="+commandDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("installer accepted an unsupported operating system:\n%s", output)
	}
	if !strings.Contains(string(output), "unsupported operating system Plan9") {
		t.Fatalf("unexpected failure:\n%s", output)
	}
}

func requireInstallerCommands(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installer is not supported on Windows")
	}
	for _, name := range []string{"/bin/sh", "curl", "tar"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is unavailable: %v", name, err)
		}
	}
}

func installerArchiveName(t *testing.T, version string) string {
	t.Helper()
	operatingSystem := runtime.GOOS
	if operatingSystem != "darwin" && operatingSystem != "linux" {
		t.Skipf("installer does not support %s", operatingSystem)
	}
	architecture := runtime.GOARCH
	if architecture != "amd64" && architecture != "arm64" {
		t.Skipf("installer does not support %s", architecture)
	}
	return fmt.Sprintf("complyscan_%s_%s_%s.tar.gz", version, operatingSystem, architecture)
}

func releaseArchive(t *testing.T, binary string) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "complyscan", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tarWriter, binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func releaseFixture(t *testing.T, tag, archiveName string, archive []byte, checksums string) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, tag)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, archiveName), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "checksums.txt"), []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}
	return fileURL(root)
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}
