package cli

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLocalizedTextUsesChineseForChineseLocale(t *testing.T) {
	setLocale(t, "zh_CN.UTF-8")

	text := localizedText()
	if !strings.Contains(text.pingLong, "参数") {
		t.Fatalf("pingLong = %q, want Chinese help text", text.pingLong)
	}
	if !strings.Contains(text.statusShort, "用量") {
		t.Fatalf("statusShort = %q, want Chinese help text", text.statusShort)
	}
}

func TestLocalizedTextFallsBackToEnglish(t *testing.T) {
	setLocale(t, "C")

	text := localizedText()
	if !strings.Contains(text.pingLong, "Arguments") {
		t.Fatalf("pingLong = %q, want English help text", text.pingLong)
	}
	if !strings.Contains(text.statusShort, "usage") {
		t.Fatalf("statusShort = %q, want English help text", text.statusShort)
	}
}

func TestLocalizedTextHonorsLocalePrecedence(t *testing.T) {
	// POSIX: LC_ALL overrides LANG, so an explicit en_US wins over zh_CN.
	setLocale(t, "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "en_US.UTF-8")

	text := localizedText()
	if !strings.Contains(text.statusShort, "usage") {
		t.Fatalf("statusShort = %q, want English when LC_ALL=en_US overrides LANG=zh_CN", text.statusShort)
	}
}

func TestRootCommandAliases(t *testing.T) {
	setLocale(t, "C")

	root := newRootCmd()
	cases := map[string]string{
		"p":      "ping",
		"sched":  "schedule",
		"s":      "status",
		"w":      "watch",
		"c":      "config",
		"cfg":    "config",
		"v":      "version",
		"ver":    "version",
		"up":     "upgrade",
		"update": "upgrade",
		"rm":     "uninstall",
		"remove": "uninstall",
	}

	for alias, want := range cases {
		cmd, _, err := root.Find([]string{alias})
		if err != nil {
			t.Fatalf("Find(%q) error = %v", alias, err)
		}
		if got := cmd.Name(); got != want {
			t.Fatalf("Find(%q) = %q, want %q", alias, got, want)
		}
	}

	nested := map[string]string{
		"i": "init",
		"p": "path",
	}
	for alias, want := range nested {
		cmd, _, err := root.Find([]string{"c", alias})
		if err != nil {
			t.Fatalf("Find(config %q) error = %v", alias, err)
		}
		if got := cmd.Name(); got != want {
			t.Fatalf("Find(config %q) = %q, want %q", alias, got, want)
		}
	}
}

func TestHelpFlagDescriptionIsLocalized(t *testing.T) {
	setLocale(t, "zh_CN.UTF-8")

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"ping", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "显示此命令的帮助") {
		t.Fatalf("help output = %q, want localized help flag", got)
	}
}

func TestRootHelpLocalizesDefaultCompletionCommand(t *testing.T) {
	setLocale(t, "zh_CN.UTF-8")

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "生成 shell 补全脚本") {
		t.Fatalf("help output = %q, want localized completion command", got)
	}
	if strings.Contains(got, "Generate the autocompletion script") {
		t.Fatalf("help output = %q, still contains default English completion text", got)
	}
}

func TestRootHelpPrintsCommandAliases(t *testing.T) {
	setLocale(t, "C")

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"ping, p",
		"status, s, stat",
		"version, v, ver",
		"upgrade, up, update",
		"uninstall, rm, remove",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("help output = %q, want command alias %q", got, want)
		}
	}
}

func TestConfigHelpPrintsSubcommandAliases(t *testing.T) {
	setLocale(t, "zh_CN.UTF-8")

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"config", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"init, i", "path, p"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help output = %q, want subcommand alias %q", got, want)
		}
	}
}

func setLocale(t *testing.T, locale string) {
	t.Helper()
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"} {
		t.Setenv(key, "")
	}
	t.Setenv("LANG", locale)
}

func TestInvokedNameRecognizesOnlyTheAlias(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/lmp":       BinaryAlias,
		"lmp.exe":                  BinaryAlias,
		"/usr/local/bin/limitping": "limitping",
		"/tmp/go-build/cli.test":   "limitping",
	}
	for argv0, want := range cases {
		t.Run(argv0, func(t *testing.T) {
			old := os.Args
			os.Args = []string{argv0}
			defer func() { os.Args = old }()
			if got := invokedName(); got != want {
				t.Fatalf("invokedName() = %q, want %q", got, want)
			}
		})
	}
}

func TestRootUsageEchoesTheInvokedName(t *testing.T) {
	setLocale(t, "C")
	old := os.Args
	os.Args = []string{"/usr/local/bin/" + BinaryAlias}
	defer func() { os.Args = old }()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if got := out.String(); !strings.Contains(got, BinaryAlias+" [command]") {
		t.Fatalf("help output = %q, want usage lines using %q", got, BinaryAlias)
	}
}

func TestHelpExamplesUseTheInvokedNameAndKeepTheProductName(t *testing.T) {
	setLocale(t, "C")
	old := os.Args
	os.Args = []string{"/usr/local/bin/" + BinaryAlias}
	defer func() { os.Args = old }()

	root := newRootCmd()
	watch, _, err := root.Find([]string{"watch"})
	if err != nil {
		t.Fatalf("find watch: %v", err)
	}
	if strings.Contains(watch.Long, "limitping watch") {
		t.Errorf("watch help still shows `limitping watch` examples under the alias:\n%s", watch.Long)
	}
	if !strings.Contains(watch.Long, BinaryAlias+" watch") {
		t.Errorf("watch help has no %q examples:\n%s", BinaryAlias+" watch", watch.Long)
	}
	// The product name is not an invocation, so prose keeps saying limitping.
	if !strings.Contains(root.Long, "limitping pings") {
		t.Errorf("root help rewrote the product name out of its prose:\n%s", root.Long)
	}
}

func TestRedeemIsReachableByItsShortAlias(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"r"})
	if err != nil {
		t.Fatalf("find %q: %v", "r", err)
	}
	if cmd.Name() != "redeem" {
		t.Fatalf("%q resolved to %q, want redeem", "r", cmd.Name())
	}
}

func TestWatchAndContinueHelpDocumentAutoRedeem(t *testing.T) {
	for _, text := range []cliText{enText, zhText} {
		if !strings.Contains(text.watchLong, "auto_redeem") {
			t.Error("watch help does not mention auto_redeem")
		}
		if !strings.Contains(text.continueLong, "auto_redeem") {
			t.Error("continue help does not mention auto_redeem")
		}
		if !strings.Contains(text.watchLong, "redeem") || !strings.Contains(text.continueLong, "redeem") {
			t.Error("watch/continue help does not point at the redeem command")
		}
	}
}

// Whichever name was typed, the root help must advertise the other one, so a
// user who only ever runs `limitping` still discovers `lmp` and vice versa.
func TestRootHelpAdvertisesBothBinaryNames(t *testing.T) {
	for _, argv0 := range []string{"/usr/local/bin/" + BinaryAlias, "/usr/local/bin/limitping"} {
		t.Run(argv0, func(t *testing.T) {
			setLocale(t, "C")
			old := os.Args
			os.Args = []string{argv0}
			defer func() { os.Args = old }()

			root := newRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{"--help"})
			if err := root.Execute(); err != nil {
				t.Fatalf("help: %v", err)
			}
			got := out.String()
			for _, want := range []string{BinaryAlias, "limitping"} {
				if !strings.Contains(got, want) {
					t.Fatalf("help output does not mention %q:\n%s", want, got)
				}
			}
			if !strings.Contains(got, "Aliases:") {
				t.Fatalf("help output has no Aliases section:\n%s", got)
			}
		})
	}
}

// Every string is set in both locales, and format strings take the same verbs.
// An unset entry is silent at compile time and only shows up at runtime as an
// empty line or a `%!(EXTRA ...)` tail, which is exactly how upgradeCurrentFmt
// shipped empty once.
func TestLocalizedTextIsCompleteInBothLocales(t *testing.T) {
	// Deliberately empty in English: it falls through to the error's own text.
	optional := map[string]bool{"statusSubAccessError": true}

	en := reflect.ValueOf(enText)
	zh := reflect.ValueOf(zhText)
	typ := en.Type()

	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if typ.Field(i).Type.Kind() != reflect.String {
			continue // the weekday array carries its own zero-value fallback
		}
		enVal, zhVal := en.Field(i).String(), zh.Field(i).String()
		if enVal == "" && !optional[name] {
			t.Errorf("enText.%s is empty", name)
		}
		if zhVal == "" {
			t.Errorf("zhText.%s is empty", name)
		}
		if enVal == "" || zhVal == "" {
			continue
		}
		if got, want := verbCount(zhVal), verbCount(enVal); got != want {
			t.Errorf("%s takes %d format verbs in en but %d in zh", name, want, got)
		}
	}
}

// verbCount counts printf verbs, treating %% as a literal percent sign.
func verbCount(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++
			continue
		}
		n++
	}
	return n
}

// The tag is meant to be the only place a version is written down, so a build
// with no release identity must say so rather than inventing a number that
// could compare against a published release.
func TestVersionResolution(t *testing.T) {
	old := Version
	defer func() { Version = old }()

	Version = "v0.10.0"
	if got := version(); got != "0.10.0" {
		t.Errorf("version() = %q, want the ldflag without its v", got)
	}
	if !isReleaseVersion() {
		t.Error("a release ldflag should read as a release")
	}

	Version = ""
	if got := version(); !strings.HasPrefix(got, DevVersion) {
		t.Errorf("version() = %q, want a %s build for the test binary", got, DevVersion)
	}
	if isReleaseVersion() {
		t.Error("a local build must not claim to be a release")
	}
}

// The tag must stay the only place a version is written down. Hardcoding a
// default here is what used to drift from it, so guard the invariant rather
// than relying on remembering it.
func TestVersionIsNotHardcodedInSource(t *testing.T) {
	src, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `var Version = ""`) {
		t.Fatal(`cli.go must declare 'var Version = ""'; a hardcoded default drifts from the release tag`)
	}
}

func TestReleaseVersionRejectsAnythingButAPublishedTag(t *testing.T) {
	for _, v := range []string{"0.10.0", "v0.10.0", "1", "1.2.3.4"} {
		if releaseVersion(v) == "" {
			t.Errorf("releaseVersion(%q) = \"\", want it accepted", v)
		}
	}
	for _, v := range []string{
		"", "dev", "dev+7599547-dirty",
		"0.9.1-0.20260909072312-7599547+dirty", // `go build` pseudo-version
		"0.10.0-rc1", "0.10.0+snapshot",
	} {
		if got := releaseVersion(v); got != "" {
			t.Errorf("releaseVersion(%q) = %q, want it rejected", v, got)
		}
	}
}
