package cfgfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type asked struct {
	calls    int
	lastDiff string
	canMerge bool
	answer   Decision
}

func setup(t *testing.T) (dir string, a *asked) {
	t.Helper()
	dir = t.TempDir()
	oldM, oldB, oldNow, oldDecide := ManifestPath, BackupRoot, Now, Decide
	ManifestPath = filepath.Join(dir, "state", "manifest.json")
	BackupRoot = filepath.Join(dir, "state", "backups")
	Now = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }
	a = &asked{answer: Keep}
	Decide = func(path, diff string, canMerge bool) Decision {
		a.calls++
		a.lastDiff, a.canMerge = diff, canMerge
		return a.answer
	}
	t.Cleanup(func() { ManifestPath, BackupRoot, Now, Decide = oldM, oldB, oldNow, oldDecide })
	return dir, a
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustWrite(t *testing.T, path, content string, o Opts) Outcome {
	t.Helper()
	out, err := Write(path, content, o)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func backups(t *testing.T) []string {
	t.Helper()
	var files []string
	_ = filepath.Walk(BackupRoot, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	return files
}

func TestWriteCreatesFileAndManifest(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "app", "docker-compose.yml")
	if out := mustWrite(t, p, "services: {}\n", Opts{Kind: KindCompose}); out != Created {
		t.Fatalf("outcome %v", out)
	}
	if read(t, p) != "services: {}\n" {
		t.Error("content")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	e, ok := Lookup(p)
	if !ok || e.Owner != OwnerInstaller || e.SHA256 != hash("services: {}\n") {
		t.Errorf("manifest entry %+v %v", e, ok)
	}
	if a.calls != 0 || len(backups(t)) != 0 {
		t.Errorf("a new file needs no question or backup: %d %v", a.calls, backups(t))
	}
	if fi, _ := os.Stat(ManifestPath); fi.Mode().Perm() != 0600 {
		t.Errorf("manifest mode %v", fi.Mode().Perm())
	}
}

func TestWriteTwiceIsByteIdenticalAndTouchesNothing(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "nginx.conf")
	mustWrite(t, p, "server {\n    listen 443;\n}\n", Opts{Kind: KindNginx})
	before, _ := os.Stat(p)
	time.Sleep(10 * time.Millisecond)
	if out := mustWrite(t, p, "server {\n    listen 443;\n}\n", Opts{Kind: KindNginx}); out != Unchanged {
		t.Errorf("outcome %v", out)
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an identical write touched the file")
	}
	if a.calls != 0 || len(backups(t)) != 0 {
		t.Errorf("asked %d, backups %v", a.calls, backups(t))
	}
}

func TestUnchangedInstallerFileIsReplacedAfterABackup(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "Caddyfile")
	mustWrite(t, p, "a.example.com {\n\trespond \"v1\"\n}\n", Opts{Kind: KindCaddy})
	out := mustWrite(t, p, "a.example.com {\n\trespond \"v2\"\n}\n", Opts{Kind: KindCaddy})
	if out != Replaced || !strings.Contains(read(t, p), "v2") || a.calls != 0 {
		t.Fatalf("outcome %v, asked %d", out, a.calls)
	}
	bs := backups(t)
	if len(bs) != 1 || !strings.Contains(read(t, bs[0]), "v1") {
		t.Fatalf("backups %v", bs)
	}
	if fi, _ := os.Stat(bs[0]); fi.Mode().Perm() != 0600 {
		t.Errorf("backup mode %v", fi.Mode().Perm())
	}
	if !strings.Contains(bs[0], "20261113-120000") || !strings.HasSuffix(bs[0], "Caddyfile") {
		t.Errorf("backup path %q", bs[0])
	}
}

func TestEditedFileIsNotReplacedWithoutAsking(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "docker-compose.yml")
	v1 := "services:\n  web:\n    image: app:1\n"
	mustWrite(t, p, v1, Opts{Kind: KindCompose})
	edited := v1 + "  mine:\n    image: mine:1\n"
	if err := os.WriteFile(p, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	v2 := "services:\n  web:\n    image: app:2\n"

	a.answer = Keep
	if out := mustWrite(t, p, v2, Opts{Kind: KindCompose}); out != Kept || read(t, p) != edited {
		t.Fatalf("keep: %v\n%s", out, read(t, p))
	}
	if a.calls != 1 || !a.canMerge || !strings.Contains(a.lastDiff, "- ") || !strings.Contains(a.lastDiff, "+     image: app:2") {
		t.Errorf("asked %d, canMerge %v, diff:\n%s", a.calls, a.canMerge, a.lastDiff)
	}
	// The same offer is not asked again.
	if out := mustWrite(t, p, v2, Opts{Kind: KindCompose}); out != Kept || a.calls != 1 {
		t.Errorf("a kept file was asked about again: %v after %d questions", out, a.calls)
	}
	// A different offer is.
	v3 := "services:\n  web:\n    image: app:3\n"
	mustWrite(t, p, v3, Opts{Kind: KindCompose})
	if a.calls != 2 {
		t.Errorf("a new offer must be asked, calls = %d", a.calls)
	}
}

func TestEditedFileReplaceAndMerge(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "docker-compose.yml")
	v1 := "services:\n  web:\n    image: app:1\n"
	mustWrite(t, p, v1, Opts{Kind: KindCompose})
	edited := v1 + "  mine:\n    image: mine:1\n"
	_ = os.WriteFile(p, []byte(edited), 0600)
	v2 := "services:\n  web:\n    image: app:2\n"

	a.answer = MergeIt
	if out := mustWrite(t, p, v2, Opts{Kind: KindCompose}); out != Merged {
		t.Fatalf("outcome %v", out)
	}
	got := read(t, p)
	if !strings.Contains(got, "app:2") || strings.Contains(got, "app:1") || !strings.Contains(got, "mine:1") {
		t.Errorf("merge result:\n%s", got)
	}
	// The merged file is the operator's now: a second run with the same offer is quiet.
	calls := a.calls
	mustWrite(t, p, v2, Opts{Kind: KindCompose})
	if a.calls != calls || read(t, p) != got {
		t.Error("a merged file was touched again")
	}

	a.answer = ReplaceIt
	_ = os.WriteFile(p, []byte(edited), 0600)
	if out := mustWrite(t, p, "services:\n  web:\n    image: app:9\n", Opts{Kind: KindCompose}); out != Replaced {
		t.Fatalf("outcome %v", out)
	}
	if got := read(t, p); strings.Contains(got, "mine") || !strings.Contains(got, "app:9") {
		t.Errorf("replace result:\n%s", got)
	}
	found := false
	for _, b := range backups(t) {
		if strings.Contains(read(t, b), "mine:1") {
			found = true
		}
	}
	if !found {
		t.Error("the operator's version was not backed up before it was replaced")
	}
}

func TestUnknownExistingFileIsAskedAbout(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "Caddyfile")
	_ = os.WriteFile(p, []byte("old.example.com {\n}\n"), 0644)
	mustWrite(t, p, "new.example.com {\n}\n", Opts{Kind: KindCaddy})
	if a.calls != 1 {
		t.Errorf("a file the installer never wrote must be asked about, calls = %d", a.calls)
	}
	if read(t, p) != "old.example.com {\n}\n" {
		t.Error("the unknown file was overwritten")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0644 {
		t.Errorf("mode changed to %v", fi.Mode().Perm())
	}
}

func TestRawFilesOfferNoMerge(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "token")
	_ = os.WriteFile(p, []byte("a"), 0600)
	a.answer = MergeIt
	out := mustWrite(t, p, "b", Opts{Kind: KindRaw})
	if a.canMerge || out != Kept || read(t, p) != "a" {
		t.Errorf("canMerge %v outcome %v content %q", a.canMerge, out, read(t, p))
	}
}

func TestEnvIsMergedByKeyWithoutAsking(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, ".env")
	first := "# panel\nAPP_PORT=3000\nJWT_AUTH_SECRET=aaa\n"
	mustWrite(t, p, first, Opts{Kind: KindEnv})
	// The operator changes a value and adds a key.
	edited := "# panel\nAPP_PORT=3100\nJWT_AUTH_SECRET=aaa\nMY_FLAG=1\n"
	_ = os.WriteFile(p, []byte(edited), 0600)
	// The next run proposes fresh random secrets and a new key.
	proposed := "# panel\nAPP_PORT=3000\nJWT_AUTH_SECRET=zzz\nNEW_KEY=hello\n"
	if out := mustWrite(t, p, proposed, Opts{Kind: KindEnv}); out != Merged {
		t.Fatalf("outcome %v", out)
	}
	got := read(t, p)
	if got != edited+"NEW_KEY=hello\n" {
		t.Errorf("merged .env:\n%s", got)
	}
	if a.calls != 0 {
		t.Error(".env is merged by key, nobody is asked")
	}
	// A second run changes nothing.
	if out := mustWrite(t, p, proposed, Opts{Kind: KindEnv}); out != Unchanged || read(t, p) != got {
		t.Errorf("second run: %v", out)
	}
}

func TestMergeEnvDetails(t *testing.T) {
	got := MergeEnv("A=1\nexport B=2\n  C = 3\n", "A=9\nB=9\nC=9\nD=4\n# comment\nE=5")
	if got != "A=1\nexport B=2\n  C = 3\nD=4\nE=5\n" {
		t.Errorf("got %q", got)
	}
	if MergeEnv("", "A=1\n") != "A=1\n" {
		t.Error("empty existing")
	}
	if MergeEnv("A=1", "A=1\n") != "A=1\n" {
		t.Error("a missing trailing newline must be added")
	}
}

const nginxUser = `server_names_hash_bucket_size 64;
client_max_body_size 50m;

# { not a block
upstream backend {
    server unix:/var/run/app.sock;
}

server {
    listen 443 ssl;
    server_name panel.example.com;
    location / { proxy_pass http://backend; }
}

server {
    listen 8080;
    server_name mine.example.com;
    location / { return 200 "mine"; }
}
`

const nginxProposed = `server_names_hash_bucket_size 64;

upstream backend {
    server unix:/var/run/app.sock;
}

server {
    listen 443 ssl;
    server_name panel.example.com;
    location / { proxy_pass http://backend; proxy_http_version 1.1; }
}
`

func TestMergeNginx(t *testing.T) {
	got := MergeNginx(nginxUser, nginxProposed)
	if !strings.Contains(got, "proxy_http_version 1.1") {
		t.Error("the installer's server block must replace the operator's")
	}
	if strings.Count(got, "server_name panel.example.com") != 1 {
		t.Errorf("the installer's server block is present twice:\n%s", got)
	}
	if !strings.Contains(got, "server_name mine.example.com") || !strings.Contains(got, `return 200 "mine"`) {
		t.Errorf("the operator's own server block was lost:\n%s", got)
	}
	if !strings.Contains(got, "client_max_body_size 50m;") {
		t.Errorf("the operator's directive was lost:\n%s", got)
	}
	if strings.Count(got, "upstream backend") != 1 {
		t.Errorf("an upstream both files define is duplicated:\n%s", got)
	}
	// Merging again changes nothing: the result is stable.
	if again := MergeNginx(got, nginxProposed); again != got {
		t.Errorf("merge is not stable:\n%s\n---\n%s", got, again)
	}
}

func TestMergeNginxServerNameOrderDoesNotMatter(t *testing.T) {
	a := "server {\n    server_name b.example.com a.example.com;\n    listen 80;\n}\n"
	b := "server {\n    listen 81;\n    server_name a.example.com b.example.com;\n}\n"
	got := MergeNginx(a, b)
	if strings.Count(got, "server {") != 1 || !strings.Contains(got, "listen 81") {
		t.Errorf("same server names in another order are one block:\n%s", got)
	}
}

const caddyUser = `{
	email admin@example.com
}

(headers) {
	header -Server
}

panel.example.com {
	import headers
	reverse_proxy localhost:3000
}

mine.example.com {
	respond "mine"
}
`

const caddyProposed = `{
	email admin@example.com
	servers { trusted_proxies static private_ranges }
}

(headers) {
	header -Server
}

panel.example.com {
	import headers
	reverse_proxy localhost:3001
}
`

func TestMergeCaddy(t *testing.T) {
	got := MergeCaddy(caddyUser, caddyProposed)
	if !strings.Contains(got, "localhost:3001") || strings.Contains(got, "localhost:3000") {
		t.Errorf("installer's site block must win:\n%s", got)
	}
	if !strings.Contains(got, `respond "mine"`) {
		t.Errorf("the operator's site was lost:\n%s", got)
	}
	if strings.Count(got, "(headers)") != 1 || strings.Count(got, "trusted_proxies") != 1 {
		t.Errorf("duplicated blocks:\n%s", got)
	}
	if again := MergeCaddy(got, caddyProposed); again != got {
		t.Error("merge is not stable")
	}
}

const composeUser = `name: remnawave
services:
    remnawave:
        image: remnawave/backend:2
        restart: always
    mine:
        image: mine/app:1
networks:
    remnawave-network:
        driver: bridge
    mynet:
        driver: bridge
volumes:
    remnawave-db-data:
        name: remnawave-db-data
    my-data:
        name: my-data
x-logging: &logging
  driver: json-file
`

const composeProposed = `name: remnawave
services:
  remnawave:
    image: remnawave/backend:2.1
    restart: always
networks:
  remnawave-network:
    driver: bridge
volumes:
  remnawave-db-data:
    name: remnawave-db-data
`

func TestMergeCompose(t *testing.T) {
	got := MergeCompose(composeUser, composeProposed)
	for _, want := range []string{"backend:2.1", "mine/app:1", "mynet", "my-data", "x-logging: &logging", "name: remnawave"} {
		if !strings.Contains(got, want) {
			t.Errorf("merge misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "backend:2\n") {
		t.Errorf("the old installer service is still there:\n%s", got)
	}
	if strings.Count(got, "remnawave-network:") != 1 || strings.Count(got, "remnawave-db-data:") != 1 {
		t.Errorf("duplicates:\n%s", got)
	}
	if again := MergeCompose(got, composeProposed); again != got {
		t.Errorf("merge is not stable:\n%s\n---\n%s", got, again)
	}
}

func TestReRunAfterAMergeKeepsTheOperatorsAdditions(t *testing.T) {
	dir, a := setup(t)
	p := filepath.Join(dir, "docker-compose.yml")
	mustWrite(t, p, composeProposed, Opts{Kind: KindCompose})
	_ = os.WriteFile(p, []byte(composeUser), 0600)
	a.answer = MergeIt
	mustWrite(t, p, composeProposed, Opts{Kind: KindCompose})
	merged := read(t, p)
	for i := 0; i < 3; i++ {
		mustWrite(t, p, composeProposed, Opts{Kind: KindCompose})
	}
	if read(t, p) != merged || a.calls != 1 {
		t.Errorf("calls %d, file changed on re-run", a.calls)
	}
}

func TestReplaceBacksUpAndRecords(t *testing.T) {
	dir, _ := setup(t)
	p := filepath.Join(dir, "nginx.conf")
	_ = os.WriteFile(p, []byte("old\n"), 0640)
	if err := Replace(p, "new\n", Opts{}); err != nil {
		t.Fatal(err)
	}
	if read(t, p) != "new\n" {
		t.Error("content")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0640 {
		t.Errorf("the file's own mode must stay, got %v", fi.Mode().Perm())
	}
	if bs := backups(t); len(bs) != 1 || read(t, bs[0]) != "old\n" {
		t.Errorf("backups %v", bs)
	}
	if e, _ := Lookup(p); e.SHA256 != hash("new\n") || e.Owner != OwnerInstaller {
		t.Errorf("entry %+v", e)
	}
	// Two changes within one second keep both backups.
	_ = Replace(p, "newer\n", Opts{})
	if len(backups(t)) != 2 {
		t.Errorf("backups %v", backups(t))
	}
	if err := Replace(p, "newer\n", Opts{}); err != nil || len(backups(t)) != 2 {
		t.Errorf("an identical replace made a backup: %v", backups(t))
	}
}

func TestWriteSecretPermissions(t *testing.T) {
	dir, _ := setup(t)
	p := filepath.Join(dir, ".secrets", "certbot", "cloudflare.ini")
	if err := WriteSecret(p, "dns_cloudflare_api_token = x\n"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
		t.Errorf("file %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Dir(p)); fi.Mode().Perm() != 0700 {
		t.Errorf("dir %v", fi.Mode().Perm())
	}
	if err := WriteSecret(p, "dns_cloudflare_api_token = y\n"); err != nil || read(t, p) != "dns_cloudflare_api_token = y\n" {
		t.Errorf("update: %v", err)
	}
}

func TestSetComposeEnv(t *testing.T) {
	dir, _ := setup(t)
	p := filepath.Join(dir, "docker-compose.yml")
	_ = os.WriteFile(p, []byte("    environment:\n      - REMNAWAVE_API_TOKEN=$api_token\n      - OTHER=1\n"), 0600)
	if err := SetComposeEnv(p, "REMNAWAVE_API_TOKEN", "ab$cd"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "    environment:\n      - REMNAWAVE_API_TOKEN=ab$cd\n      - OTHER=1\n" {
		t.Errorf("got %q", got)
	}
	if err := SetComposeEnv(p, "MISSING", "x"); err == nil {
		t.Error("a missing key must be an error")
	}
	if len(backups(t)) != 1 {
		t.Errorf("backups %v", backups(t))
	}
}

func TestStableAndForget(t *testing.T) {
	dir, _ := setup(t)
	n := 0
	gen := func() string { n++; return strings.Repeat("x", n) }
	a, _ := Stable("remnawave.cookie1", gen)
	b, _ := Stable("remnawave.cookie1", gen)
	c, _ := Stable("remnawave.cookie2", gen)
	if a != b || a == c || n != 2 {
		t.Errorf("a=%q b=%q c=%q n=%d", a, b, c, n)
	}
	p := filepath.Join(dir, "opt", "remnawave", ".env")
	mustWrite(t, p, "A=1\n", Opts{Kind: KindEnv})
	if err := Forget(filepath.Join(dir, "opt", "remnawave"), "remnawave."); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup(p); ok {
		t.Error("the file entry survived Forget")
	}
	d, _ := Stable("remnawave.cookie1", gen)
	if d == a {
		t.Error("a forgotten value was reused")
	}
}

func TestDiff(t *testing.T) {
	if got := Diff("a\nb\nc\n", "a\nB\nc\n"); got != "  a\n- b\n+ B\n  c" {
		t.Errorf("got %q", got)
	}
	if got := Diff("a\n", "a\n"); got != "" {
		t.Errorf("identical files: %q", got)
	}
	var old, new []string
	for i := 0; i < 300; i++ {
		old = append(old, "same")
		new = append(new, "diff")
	}
	got := Diff(strings.Join(old, "\n"), strings.Join(new, "\n"))
	if !strings.Contains(got, "more lines") || strings.Count(got, "\n") > maxDiffLines+1 {
		t.Errorf("a long diff must be cut (%d lines)", strings.Count(got, "\n"))
	}
}

// Killing the process between the temporary file and the rename leaves the
// original file untouched.
func TestKillBetweenTempFileAndRenameKeepsTheOriginal(t *testing.T) {
	if os.Getenv("CFGFILE_CRASH_CHILD") == "1" {
		ManifestPath = os.Getenv("CFGFILE_MANIFEST")
		BackupRoot = os.Getenv("CFGFILE_BACKUPS")
		beforeRename = func() { os.Exit(3) }
		_, _ = Write(os.Getenv("CFGFILE_TARGET"), "NEW CONTENT\n", Opts{Kind: KindRaw})
		os.Exit(0) // not reached when the hook fires
	}
	dir, a := setup(t)
	p := filepath.Join(dir, "docker-compose.yml")
	mustWrite(t, p, "ORIGINAL\n", Opts{Kind: KindRaw})
	_ = a

	cmd := exec.Command(os.Args[0], "-test.run=^TestKillBetweenTempFileAndRenameKeepsTheOriginal$")
	cmd.Env = append(os.Environ(), "CFGFILE_CRASH_CHILD=1", "CFGFILE_MANIFEST="+ManifestPath,
		"CFGFILE_BACKUPS="+BackupRoot, "CFGFILE_TARGET="+p)
	err := cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 3 {
		t.Fatalf("the child did not stop at the hook: %v", err)
	}
	if got := read(t, p); got != "ORIGINAL\n" {
		t.Fatalf("the original file was damaged: %q", got)
	}
	// The abandoned temporary file is the only trace, and the next write works.
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".docker-compose.yml.tmp-*"))
	if len(leftovers) != 1 || read(t, leftovers[0]) != "NEW CONTENT\n" {
		t.Errorf("leftovers %v", leftovers)
	}
	mustWrite(t, p, "ORIGINAL\n", Opts{Kind: KindRaw})
	if e, _ := Lookup(p); e.SHA256 != hash("ORIGINAL\n") {
		t.Errorf("manifest %+v", e)
	}
}

func TestFailedRenameLeavesNoTempFileAndKeepsTheOriginal(t *testing.T) {
	dir, _ := setup(t)
	p := filepath.Join(dir, "f")
	mustWrite(t, p, "ORIGINAL\n", Opts{})
	if err := os.Mkdir(filepath.Join(dir, "g"), 0755); err != nil {
		t.Fatal(err)
	}
	// Renaming a file over a directory fails.
	if err := atomicWrite(filepath.Join(dir, "g"), []byte("x"), 0600); err == nil {
		t.Fatal("rename over a directory worked")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".g.tmp-*")); len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	if read(t, p) != "ORIGINAL\n" {
		t.Error("original changed")
	}
}

func TestCorruptManifestIsAnError(t *testing.T) {
	dir, _ := setup(t)
	_ = os.MkdirAll(filepath.Dir(ManifestPath), 0700)
	_ = os.WriteFile(ManifestPath, []byte("{not json"), 0600)
	if _, err := Write(filepath.Join(dir, "x"), "y", Opts{}); err == nil {
		t.Error("a corrupt manifest was ignored")
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Error("a file was written although the manifest could not be read")
	}
}

func TestWriteSecretReportsError(t *testing.T) {
	dir, _ := setup(t)
	blocker := filepath.Join(dir, "blocker")
	_ = os.WriteFile(blocker, nil, 0600)
	if err := WriteSecret(filepath.Join(blocker, "x.ini"), "x"); err == nil {
		t.Error("expected an error when the directory cannot be created")
	}
}

// A flow draws its random values through Stable and writes its files through
// Write, so running it again leaves every file byte-identical and asks
// nothing.
func TestAFlowRunTwiceLeavesEveryFileIdentical(t *testing.T) {
	dir, a := setup(t)
	n := 0
	random := func() string { n++; return strings.Repeat("r", n) }
	runFlow := func() {
		cookie1 := StableOr("remnawave.cookie1", random)
		cookie2 := StableOr("remnawave.cookie2", random)
		secret := StableOr("remnawave.app_secret", random)
		env := "APP_PORT=3000\nJWT_AUTH_SECRET=" + secret + "\n"
		nginx := "server {\n    server_name panel.example.com;\n    location / { set $c \"" + cookie1 + "=" + cookie2 + "\"; }\n}\n"
		caddy := "panel.example.com {\n\t@auth { query " + cookie1 + "=" + cookie2 + " }\n}\n"
		compose := "services:\n  panel:\n    image: app:1\n"
		for name, c := range map[string]struct {
			content string
			kind    Kind
		}{".env": {env, KindEnv}, "nginx.conf": {nginx, KindNginx}, "Caddyfile": {caddy, KindCaddy}, "docker-compose.yml": {compose, KindCompose}} {
			if _, err := Write(filepath.Join(dir, "opt", name), c.content, Opts{Kind: c.kind}); err != nil {
				t.Fatal(err)
			}
		}
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		files, _ := filepath.Glob(filepath.Join(dir, "opt", "*"))
		files2, _ := filepath.Glob(filepath.Join(dir, "opt", ".env"))
		for _, f := range append(files, files2...) {
			out[f] = read(t, f)
		}
		return out
	}
	runFlow()
	first := snapshot()
	runFlow()
	runFlow()
	second := snapshot()
	if len(first) != 4 {
		t.Fatalf("files: %v", first)
	}
	for f, c := range first {
		if second[f] != c {
			t.Errorf("%s changed between runs:\n%s\n---\n%s", f, c, second[f])
		}
	}
	if a.calls != 0 || len(backups(t)) != 0 || n != 3 {
		t.Errorf("questions %d, backups %v, random draws %d", a.calls, backups(t), n)
	}
}

// Nothing may write the generated files or credentials around cfgfile.
func TestNoRawWritesOfManagedFiles(t *testing.T) {
	banned := []string{".env", "docker-compose.yml", "nginx.conf", "Caddyfile", "confPath", "caddyfilePath", "composePath", "envPath", "cloudflare.ini"}
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "cfgfile" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, _ := os.ReadFile(path)
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.Contains(line, "os.WriteFile(") {
				continue
			}
			for _, b := range banned {
				if strings.Contains(line, b) {
					t.Errorf("%s:%d writes %s around cfgfile: %s", path, i+1, b, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The random values of the install flows come from Stable.
func TestFlowsDrawRandomValuesFromStable(t *testing.T) {
	flows := []string{"panelfull/install_panel.go", "panelonly/install_panel.go", "caddypanelfull/install_panel.go", "caddypanelonly/install_panel.go"}
	for _, f := range flows {
		src, err := os.ReadFile(filepath.Join("..", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"cookiesRandom1", "cookiesRandom2", "metricsUser", "metricsPass", "appSecret"} {
			for _, line := range strings.Split(string(src), "\n") {
				if strings.Contains(line, field+":") && strings.Contains(line, "genutil.") && !strings.Contains(line, "StableOr") {
					t.Errorf("%s: %s is generated directly: %s", f, field, strings.TrimSpace(line))
				}
			}
		}
	}
}
