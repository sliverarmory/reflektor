//go:build bof

package reflektor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	reflektor "github.com/sliverarmory/reflektor"
)

const (
	bofCorpusDirectoryEnv = "REFLEKTOR_BOF_CORPUS_DIR"
	bofCorpusArtifactEnv  = "REFLEKTOR_BOF_CORPUS_ARTIFACT"
	bofCorpusFixtureFile  = "REFLEKTOR_BOF_FIXTURE_FILE"
	bofCorpusFixtureDir   = "REFLEKTOR_BOF_FIXTURE_DIR"
	bofCorpusManifestPath = "testdata/e2e-manifest.json"
	bofCorpusTimeout      = 30 * time.Second
)

type bofCorpusManifest struct {
	Version    int                 `json:"version"`
	Entrypoint string              `json:"entrypoint"`
	Fixtures   bofCorpusFixtures   `json:"fixtures"`
	Artifacts  []bofCorpusArtifact `json:"artifacts"`
}

type bofCorpusFixtures struct {
	File      bofCorpusFileFixture      `json:"file"`
	Directory bofCorpusDirectoryFixture `json:"directory"`
}

type bofCorpusFileFixture struct {
	Variable     string `json:"variable"`
	ContentsUTF8 string `json:"contents_utf8"`
}

type bofCorpusDirectoryFixture struct {
	Variable string            `json:"variable"`
	Files    map[string]string `json:"files"`
}

type bofCorpusArtifact struct {
	Name   string                `json:"name"`
	OS     string                `json:"os"`
	Arch   string                `json:"arch"`
	Path   string                `json:"path"`
	Args   []bofCorpusArgument   `json:"args"`
	Expect bofCorpusExpectations `json:"expect"`
}

type bofCorpusArgument struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type bofCorpusExpectations struct {
	Types           []int    `json:"types"`
	ContainsAny     []string `json:"contains_any"`
	CaseInsensitive bool     `json:"case_insensitive"`
	MinCallbacks    int      `json:"min_callbacks"`
}

// TestSituationalAwarenessBOFCorpus executes every BOF published for the
// current host by the companion Situational-Awareness-BOFs repository. Each
// object runs in its own subprocess so a native fault names the exact artifact
// instead of terminating the entire matrix test without useful context.
func TestSituationalAwarenessBOFCorpus(t *testing.T) {
	repository := os.Getenv(bofCorpusDirectoryEnv)
	if repository == "" {
		t.Skipf("%s is not set", bofCorpusDirectoryEnv)
	}
	repository, err := filepath.Abs(repository)
	if err != nil {
		t.Fatalf("resolve BOF corpus directory: %v", err)
	}

	manifest := readBOFCorpusManifest(t, repository)
	validateBOFCorpusManifest(t, repository, manifest)
	hostArtifacts := hostBOFCorpusArtifacts(manifest)
	if len(hostArtifacts) == 0 {
		t.Fatalf("manifest has no BOFs for supported host %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	assertExactHostBOFCorpus(t, repository, hostArtifacts)

	fixtureFile, fixtureDirectory := prepareBOFCorpusFixtures(t, manifest.Fixtures)
	for _, artifact := range hostArtifacts {
		artifact := artifact
		t.Run(artifact.Name, func(t *testing.T) {
			runBOFCorpusArtifact(t, repository, artifact, fixtureFile, fixtureDirectory)
		})
	}
}

// TestSituationalAwarenessBOFCorpusChild is selected directly by the parent
// process. In ordinary go test runs it returns without adding another skip.
func TestSituationalAwarenessBOFCorpusChild(t *testing.T) {
	artifactPath := os.Getenv(bofCorpusArtifactEnv)
	if artifactPath == "" {
		return
	}
	repository := os.Getenv(bofCorpusDirectoryEnv)
	if repository == "" {
		t.Fatalf("%s is not set in corpus child", bofCorpusDirectoryEnv)
	}

	manifest := readBOFCorpusManifest(t, repository)
	var artifact *bofCorpusArtifact
	for index := range manifest.Artifacts {
		candidate := &manifest.Artifacts[index]
		if filepath.ToSlash(candidate.Path) == filepath.ToSlash(artifactPath) {
			artifact = candidate
			break
		}
	}
	if artifact == nil {
		t.Fatalf("artifact %q is absent from %s", artifactPath, bofCorpusManifestPath)
	}
	if artifact.OS != runtime.GOOS || artifact.Arch != runtime.GOARCH {
		t.Fatalf("artifact %q targets %s/%s, child is %s/%s", artifact.Path, artifact.OS, artifact.Arch, runtime.GOOS, runtime.GOARCH)
	}

	arguments := packBOFCorpusArguments(t, artifact.Args)
	objectPath, err := secureCorpusJoin(repository, artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reflektor.LoadBOFFile(objectPath)
	if err != nil {
		t.Fatalf("LoadBOFFile(%s): %v", artifact.Path, err)
	}
	outputs, executeErr := loaded.Execute(arguments)
	closeErr := loaded.Close()
	if executeErr != nil {
		t.Fatalf("Execute(%s): %v", artifact.Path, executeErr)
	}
	if closeErr != nil {
		t.Fatalf("Close(%s): %v", artifact.Path, closeErr)
	}
	assertBOFCorpusOutput(t, *artifact, outputs)
	t.Logf("executed %s: %d callback(s), %d byte(s)", artifact.Path, len(outputs), bofOutputLength(outputs))
}

func readBOFCorpusManifest(t *testing.T, repository string) bofCorpusManifest {
	t.Helper()
	manifestPath, err := secureCorpusJoin(repository, bofCorpusManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read BOF corpus manifest %s: %v", manifestPath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest bofCorpusManifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode BOF corpus manifest %s: %v", manifestPath, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		t.Fatalf("decode BOF corpus manifest %s: %v", manifestPath, err)
	}
	return manifest
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if err == nil {
		return errors.New("multiple JSON values")
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func validateBOFCorpusManifest(t *testing.T, repository string, manifest bofCorpusManifest) {
	t.Helper()
	if manifest.Version != 1 {
		t.Fatalf("BOF corpus manifest version = %d, want 1", manifest.Version)
	}
	if manifest.Entrypoint != "go" {
		t.Fatalf("BOF corpus entrypoint = %q, want %q", manifest.Entrypoint, "go")
	}
	if len(manifest.Artifacts) == 0 {
		t.Fatal("BOF corpus manifest contains no artifacts")
	}
	if manifest.Fixtures.File.Variable != "FIXTURE_FILE" {
		t.Fatalf("fixtures.file.variable = %q, want FIXTURE_FILE", manifest.Fixtures.File.Variable)
	}
	if manifest.Fixtures.Directory.Variable != "FIXTURE_DIR" {
		t.Fatalf("fixtures.directory.variable = %q, want FIXTURE_DIR", manifest.Fixtures.Directory.Variable)
	}
	if len(manifest.Fixtures.Directory.Files) == 0 {
		t.Fatal("fixtures.directory.files is empty")
	}
	for relative := range manifest.Fixtures.Directory.Files {
		if _, err := secureCorpusJoin(repository, relative); err != nil {
			t.Fatalf("fixtures.directory.files[%q]: %v", relative, err)
		}
	}

	seenPaths := make(map[string]struct{}, len(manifest.Artifacts))
	seenTargets := make(map[string]struct{}, len(manifest.Artifacts))
	for index, artifact := range manifest.Artifacts {
		location := fmt.Sprintf("artifacts[%d]", index)
		if artifact.Name == "" || strings.ContainsAny(artifact.Name, `/\\`) {
			t.Fatalf("%s.name = %q, want one path-free name", location, artifact.Name)
		}
		if !validBOFCorpusTarget(artifact.OS, artifact.Arch) {
			t.Fatalf("%s has unsupported target %q/%q", location, artifact.OS, artifact.Arch)
		}
		path := filepath.ToSlash(artifact.Path)
		wantPath := fmt.Sprintf("dist/%s/%s/%s.o", artifact.OS, artifact.Arch, artifact.Name)
		if path != wantPath {
			t.Fatalf("%s.path = %q, want %q", location, artifact.Path, wantPath)
		}
		if _, err := secureCorpusJoin(repository, artifact.Path); err != nil {
			t.Fatalf("%s.path: %v", location, err)
		}
		if _, duplicate := seenPaths[path]; duplicate {
			t.Fatalf("duplicate BOF corpus path %q", path)
		}
		seenPaths[path] = struct{}{}
		targetName := artifact.OS + "/" + artifact.Arch + "/" + artifact.Name
		if _, duplicate := seenTargets[targetName]; duplicate {
			t.Fatalf("duplicate BOF corpus target/name %q", targetName)
		}
		seenTargets[targetName] = struct{}{}
		validateBOFCorpusArguments(t, location, artifact.Args)
		if artifact.Expect.MinCallbacks < 0 {
			t.Fatalf("%s.expect.min_callbacks = %d, want non-negative", location, artifact.Expect.MinCallbacks)
		}
		if len(artifact.Expect.Types) == 0 {
			t.Fatalf("%s.expect.types is empty", location)
		}
		seenTypes := make(map[int]struct{}, len(artifact.Expect.Types))
		for _, outputType := range artifact.Expect.Types {
			if outputType < 0 {
				t.Fatalf("%s.expect.types contains negative type %d", location, outputType)
			}
			if _, duplicate := seenTypes[outputType]; duplicate {
				t.Fatalf("%s.expect.types contains duplicate type %d", location, outputType)
			}
			seenTypes[outputType] = struct{}{}
		}
		for _, substring := range artifact.Expect.ContainsAny {
			if substring == "" {
				t.Fatalf("%s.expect.contains_any contains an empty string", location)
			}
		}
	}
}

func validateBOFCorpusArguments(t *testing.T, location string, arguments []bofCorpusArgument) {
	t.Helper()
	for index, argument := range arguments {
		argumentLocation := fmt.Sprintf("%s.args[%d]", location, index)
		switch argument.Type {
		case "string", "wstring":
			var value string
			if err := json.Unmarshal(argument.Value, &value); err != nil {
				t.Fatalf("%s.value must be a string: %v", argumentLocation, err)
			}
			if err := validateCorpusVariables(value); err != nil {
				t.Fatalf("%s.value: %v", argumentLocation, err)
			}
		case "int32":
			if _, err := parseCorpusInteger(argument.Value, 32); err != nil {
				t.Fatalf("%s.value: %v", argumentLocation, err)
			}
		case "int16":
			if _, err := parseCorpusInteger(argument.Value, 16); err != nil {
				t.Fatalf("%s.value: %v", argumentLocation, err)
			}
		default:
			t.Fatalf("%s.type = %q, want string, wstring, int32, or int16", argumentLocation, argument.Type)
		}
	}
}

func validBOFCorpusTarget(goos string, goarch string) bool {
	switch goos + "/" + goarch {
	case "darwin/amd64", "darwin/arm64", "linux/386", "linux/amd64", "linux/arm64", "windows/386", "windows/amd64", "windows/arm64":
		return true
	default:
		return false
	}
}

func hostBOFCorpusArtifacts(manifest bofCorpusManifest) []bofCorpusArtifact {
	var result []bofCorpusArtifact
	for _, artifact := range manifest.Artifacts {
		if artifact.OS == runtime.GOOS && artifact.Arch == runtime.GOARCH {
			result = append(result, artifact)
		}
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].Path < result[right].Path })
	return result
}

func assertExactHostBOFCorpus(t *testing.T, repository string, manifestArtifacts []bofCorpusArtifact) {
	t.Helper()
	directory, err := secureCorpusJoin(repository, filepath.Join("dist", runtime.GOOS, runtime.GOARCH))
	if err != nil {
		t.Fatal(err)
	}
	discovered := make(map[string]struct{})
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".o") {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("BOF artifact is not a regular file: %s", path)
		}
		relative, relativeErr := filepath.Rel(repository, path)
		if relativeErr != nil {
			return relativeErr
		}
		discovered[filepath.ToSlash(relative)] = struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("discover host BOF corpus in %s: %v", directory, err)
	}
	wanted := make(map[string]struct{}, len(manifestArtifacts))
	for _, artifact := range manifestArtifacts {
		wanted[filepath.ToSlash(artifact.Path)] = struct{}{}
	}
	missing := setDifference(wanted, discovered)
	extra := setDifference(discovered, wanted)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("BOF corpus does not exactly match manifest for %s/%s: missing=%v extra=%v", runtime.GOOS, runtime.GOARCH, missing, extra)
	}
}

func setDifference(left map[string]struct{}, right map[string]struct{}) []string {
	var difference []string
	for value := range left {
		if _, present := right[value]; !present {
			difference = append(difference, value)
		}
	}
	sort.Strings(difference)
	return difference
}

func prepareBOFCorpusFixtures(t *testing.T, fixtures bofCorpusFixtures) (string, string) {
	t.Helper()
	root := t.TempDir()
	filePath := filepath.Join(root, "fixture.txt")
	directoryPath := filepath.Join(root, "directory")
	if err := os.MkdirAll(directoryPath, 0o700); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(fixtures.File.ContentsUTF8), 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	for relative, contents := range fixtures.Directory.Files {
		path, err := secureCorpusJoin(directoryPath, relative)
		if err != nil {
			t.Fatalf("prepare directory fixture %q: %v", relative, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create directory fixture parent for %q: %v", relative, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write directory fixture %q: %v", relative, err)
		}
	}
	return filePath, directoryPath
}

func runBOFCorpusArtifact(t *testing.T, repository string, artifact bofCorpusArtifact, fixtureFile string, fixtureDirectory string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), bofCorpusTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSituationalAwarenessBOFCorpusChild$", "-test.count=1", "-test.v")
	command.Env = overrideEnv(os.Environ(), map[string]string{
		bofCorpusDirectoryEnv: repository,
		bofCorpusArtifactEnv:  filepath.ToSlash(artifact.Path),
		bofCorpusFixtureFile:  fixtureFile,
		bofCorpusFixtureDir:   fixtureDirectory,
	})
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("BOF %s timed out after %s\n%s", artifact.Path, bofCorpusTimeout, output)
	}
	if err != nil {
		t.Fatalf("BOF %s subprocess failed: %v\n%s", artifact.Path, err, output)
	}
	t.Logf("%s", bytes.TrimSpace(output))
}

func packBOFCorpusArguments(t *testing.T, arguments []bofCorpusArgument) []byte {
	t.Helper()
	var packed reflektor.BOFArguments
	for index, argument := range arguments {
		var err error
		switch argument.Type {
		case "string", "wstring":
			var value string
			if decodeErr := json.Unmarshal(argument.Value, &value); decodeErr != nil {
				t.Fatalf("decode argument %d: %v", index, decodeErr)
			}
			value, err = expandCorpusVariables(value)
			if err == nil && argument.Type == "string" {
				err = packed.AddString(value)
			} else if err == nil {
				err = packed.AddUTF16String(value)
			}
		case "int32":
			var value int64
			value, err = parseCorpusInteger(argument.Value, 32)
			if err == nil {
				err = packed.AddInt32(int32(value))
			}
		case "int16":
			var value int64
			value, err = parseCorpusInteger(argument.Value, 16)
			if err == nil {
				err = packed.AddInt16(int16(value))
			}
		default:
			err = fmt.Errorf("unsupported argument type %q", argument.Type)
		}
		if err != nil {
			t.Fatalf("pack argument %d (%s): %v", index, argument.Type, err)
		}
	}
	return packed.Bytes()
}

func parseCorpusInteger(raw json.RawMessage, bits int) (int64, error) {
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		var text string
		if stringErr := json.Unmarshal(raw, &text); stringErr != nil {
			return 0, fmt.Errorf("want signed %d-bit integer: %w", bits, err)
		}
		value, parseErr := strconv.ParseInt(text, 0, bits)
		if parseErr != nil {
			return 0, fmt.Errorf("want signed %d-bit integer: %w", bits, parseErr)
		}
		return value, nil
	}
	value, err := strconv.ParseInt(number.String(), 10, bits)
	if err != nil {
		return 0, fmt.Errorf("want signed %d-bit integer: %w", bits, err)
	}
	return value, nil
}

func validateCorpusVariables(value string) error {
	_, err := replaceCorpusVariables(value, map[string]string{
		"${FIXTURE_FILE}": "fixture-file",
		"${FIXTURE_DIR}":  "fixture-directory",
	})
	return err
}

func expandCorpusVariables(value string) (string, error) {
	return replaceCorpusVariables(value, map[string]string{
		"${FIXTURE_FILE}": os.Getenv(bofCorpusFixtureFile),
		"${FIXTURE_DIR}":  os.Getenv(bofCorpusFixtureDir),
	})
}

func replaceCorpusVariables(value string, replacements map[string]string) (string, error) {
	for variable, replacement := range replacements {
		value = strings.ReplaceAll(value, variable, replacement)
	}
	if strings.Contains(value, "${") {
		return "", fmt.Errorf("unknown corpus variable in %q", value)
	}
	return value, nil
}

func assertBOFCorpusOutput(t *testing.T, artifact bofCorpusArtifact, outputs []reflektor.BOFOutput) {
	t.Helper()
	if len(outputs) < artifact.Expect.MinCallbacks {
		t.Fatalf("%s emitted %d callback(s), want at least %d", artifact.Path, len(outputs), artifact.Expect.MinCallbacks)
	}
	allowedTypes := make(map[int]struct{}, len(artifact.Expect.Types))
	for _, outputType := range artifact.Expect.Types {
		allowedTypes[outputType] = struct{}{}
	}
	var combined bytes.Buffer
	for index, output := range outputs {
		if _, allowed := allowedTypes[output.Type]; !allowed {
			t.Fatalf("%s callback %d type = %d, want one of %v", artifact.Path, index, output.Type, artifact.Expect.Types)
		}
		combined.Write(output.Data)
	}
	if len(artifact.Expect.ContainsAny) == 0 {
		return
	}
	haystack := combined.String()
	if artifact.Expect.CaseInsensitive {
		haystack = strings.ToLower(haystack)
	}
	for _, substring := range artifact.Expect.ContainsAny {
		if artifact.Expect.CaseInsensitive {
			substring = strings.ToLower(substring)
		}
		if strings.Contains(haystack, substring) {
			return
		}
	}
	t.Fatalf("%s output did not contain any of %q; output=%q", artifact.Path, artifact.Expect.ContainsAny, truncateBOFOutput(combined.Bytes(), 4096))
}

func bofOutputLength(outputs []reflektor.BOFOutput) int {
	total := 0
	for _, output := range outputs {
		total += len(output.Data)
	}
	return total
}

func truncateBOFOutput(value []byte, maximum int) []byte {
	if len(value) <= maximum {
		return value
	}
	truncated := append([]byte(nil), value[:maximum]...)
	return append(truncated, []byte("...[truncated]")...)
}

func secureCorpusJoin(root string, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q must be non-empty and relative", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes its root", relative)
	}
	joined := filepath.Join(root, clean)
	relativeToRoot, err := filepath.Rel(root, joined)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes its root", relative)
	}
	return joined, nil
}
