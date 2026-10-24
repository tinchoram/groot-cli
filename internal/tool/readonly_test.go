package tool

import "testing"

func TestClassifyShell(t *testing.T) {
	cases := []struct {
		cmd  string
		want Risk
	}{
		// read-only pipelines and listings
		{"ls -la", RiskRead},
		{"git log | head", RiskRead},
		{"git status && git diff", RiskRead},
		{"rg foo | wc -l", RiskRead},
		{"sed s/a/b/ f", RiskRead},
		{"docker ps", RiskRead},
		{"kubectl get pods -A", RiskRead},
		{"kubectl get pods -n prod", RiskRead},
		{"find . -name '*.go'", RiskRead},
		{"awk '{print $1}' f", RiskRead},
		{"pwd", RiskRead},
		{"echo hi", RiskRead},
		{"go version", RiskRead},
		{"env", RiskRead},
		{"git branch", RiskRead},
		{"git branch -a", RiskRead},

		// anything that writes, changes state, or runs another program
		{"ls; rm -rf /", RiskExec},
		{"cat a > b", RiskExec},
		{"cat a >> b", RiskExec},
		{"ls 2> err", RiskExec},
		{"git push", RiskExec},
		{"git push --force", RiskExec},
		{"git branch -D old", RiskExec},
		{"git log --output=out.txt", RiskExec},
		{"kubectl delete pod x", RiskExec},
		{"echo $(rm x)", RiskExec},
		{"echo `rm x`", RiskExec},
		{"cat <(ls)", RiskExec},
		{"find . -delete", RiskExec},
		{"find . -exec rm {} ;", RiskExec},
		{"sed -i s/a/b/ f", RiskExec},
		{"docker rm x", RiskExec},
		{"sudo ls", RiskExec},
		{"rm x", RiskExec},
		{"curl -o out http://x", RiskExec},
		{"curl http://x", RiskExec},
		{"sh -c 'rm x'", RiskExec},
		{"bash -c ls", RiskExec},
		{"xargs rm", RiskExec},
		{"eval ls", RiskExec},
		{"ls | tee out", RiskExec},
		{"env FOO=1 rm x", RiskExec},
		{"awk '{system(\"rm x\")}' f", RiskExec},
		{"go test ./...", RiskExec},
		{"ls\nrm x", RiskExec},
		{"", RiskExec},
	}
	for _, c := range cases {
		if got := ClassifyShell(c.cmd); got != c.want {
			t.Errorf("ClassifyShell(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestBashRiskForClassifiesPerCall(t *testing.T) {
	b := NewBash("")
	if got := b.RiskFor(mustJSON(t, map[string]string{"command": "ls -la"})); got != RiskRead {
		t.Fatalf("read-only command RiskFor = %v", got)
	}
	if got := b.RiskFor(mustJSON(t, map[string]string{"command": "rm -rf x"})); got != RiskExec {
		t.Fatalf("destructive command RiskFor = %v", got)
	}
	if got := b.RiskFor("not json"); got != RiskExec {
		t.Fatal("unreadable input must be treated as exec")
	}
	if b.Risk() != RiskExec {
		t.Fatal("the static risk of bash stays exec")
	}
}

func TestOtherToolsReportTheirStaticRisk(t *testing.T) {
	if NewReadFile("").RiskFor("{}") != RiskRead {
		t.Fatal("read_file is always read")
	}
	if NewWriteFile("").RiskFor("{}") != RiskWrite {
		t.Fatal("write_file is always write")
	}
}
