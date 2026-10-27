package tool

import (
	"strings"
	"testing"
)

func TestClassifyDestructive(t *testing.T) {
	cases := []struct {
		cmd  string
		want DestructiveKind // empty means not destructive
	}{
		// delete: recursive or broad removal
		{"rm -rf ./build", DestructiveDelete},
		{"rm -fr build", DestructiveDelete},
		{"rm -r dir", DestructiveDelete},
		{"rm -R dir", DestructiveDelete},
		{"rm --recursive dir", DestructiveDelete},
		{"rm -rf /", DestructiveDelete},
		{"rm /", DestructiveDelete},
		{"rm ~/", DestructiveDelete},
		{"rm $HOME", DestructiveDelete},
		{"rm ..", DestructiveDelete},
		{"rm *", DestructiveDelete},
		{"rm .", DestructiveDelete},
		{"rm file.txt", ""},
		{"rm -f file.txt", ""},
		{"rm", ""},
		{"find . -name '*.log' -delete", DestructiveDelete},
		{"find . -exec rm {} ;", DestructiveDelete},
		{"shred secret.txt", DestructiveDelete},
		{"cd build && rm -rf .", DestructiveDelete},
		{"ls | xargs rm -rf", DestructiveDelete},
		{"xargs -0 rm -rf", DestructiveDelete},
		{"sudo -u root rm -rf /tmp/x", DestructiveDelete},
		{"sudo -E rm -rf x", DestructiveDelete},
		{"env FOO=1 rm -r x", DestructiveDelete},
		{"env -i rm -rf x", DestructiveDelete},
		{"FOO=bar rm -rf x", DestructiveDelete},
		{"nohup rm -rf x &", DestructiveDelete},
		{"time rm -rf x", DestructiveDelete},
		{"nice -n 10 rm -rf x", DestructiveDelete},
		{"timeout 5s rm -rf x", DestructiveDelete},
		{"command rm -rf x", DestructiveDelete},
		{"exec rm -rf x", DestructiveDelete},
		{"doas rm -rf x", DestructiveDelete},
		{"sh -c 'rm -rf x'", DestructiveDelete},
		{"bash -c \"cd x && rm -rf y\"", DestructiveDelete},
		{"zsh -c 'shred f'", DestructiveDelete},
		{"eval \"rm -rf x\"", DestructiveDelete},
		{"echo $(rm -rf x)", DestructiveDelete},
		{"(cd x && rm -rf y)", DestructiveDelete},
		{"ls; rm -rf /", DestructiveDelete},
		{"echo \"rm -rf /\"", ""},
		{"echo \"rm -rf x\" | cat", ""},

		// git: history or work-tree destroyers
		{"git push --force origin main", DestructiveGit},
		{"git push -f", DestructiveGit},
		{"git push --force-with-lease", DestructiveGit},
		{"git push origin :feature", DestructiveGit},
		{"git push -fu origin main", DestructiveGit},
		{"git push origin --delete feature", DestructiveGit},
		{"git push", ""},
		{"git push origin main", ""},
		{"git reset --hard HEAD~1", DestructiveGit},
		{"git reset --soft HEAD~1", ""},
		{"git clean -fd", DestructiveGit},
		{"git clean -fdx", DestructiveGit},
		{"git clean -n", ""},
		{"git checkout -- .", DestructiveGit},
		{"git checkout .", DestructiveGit},
		{"git restore .", DestructiveGit},
		{"git restore --staged .", ""},
		{"git branch -D old", DestructiveGit},
		{"git branch --delete --force old", DestructiveGit},
		{"git branch -d old", ""},
		{"git tag -d v1", DestructiveGit},
		{"git stash drop", DestructiveGit},
		{"git stash clear", DestructiveGit},
		{"git stash list", ""},
		{"git reflog expire --expire=now --all", DestructiveGit},
		{"git gc --prune=now", DestructiveGit},
		{"git filter-branch --tree-filter x", DestructiveGit},
		{"git filter-repo --path x", DestructiveGit},
		{"git update-ref -d refs/heads/x", DestructiveGit},
		{"git -C repo reset --hard", DestructiveGit},
		{"git --git-dir=.git --work-tree=. clean -f", DestructiveGit},
		{"git -c user.name=x push --force", DestructiveGit},
		{"sh -c \"git push --force\"", DestructiveGit},
		{"git log --all", ""},
		{"git status && git diff", ""},
		{"git commit -m 'reset --hard'", ""},
		{"git commit -m \"reset --hard\"", ""},

		// database: destructive statements through a client
		{"psql -c \"DROP TABLE t\"", DestructiveDatabase},
		{"psql <<SQL\nDROP TABLE t;\nSQL", DestructiveDatabase},
		{"mysql -e \"truncate table users\"", DestructiveDatabase},
		{"mysql -e \"DELETE FROM users\"", DestructiveDatabase},
		{"sqlite3 app.db \"drop table t\"", DestructiveDatabase},
		{"mongosh --eval \"db.users.deleteMany({})\"", DestructiveDatabase},
		{"mongo --eval \"db.dropDatabase()\"", DestructiveDatabase},
		{"mongosh --eval \"db.x.drop()\"", DestructiveDatabase},
		{"redis-cli FLUSHALL", DestructiveDatabase},
		{"redis-cli flushdb", DestructiveDatabase},
		{"dropdb mydb", DestructiveDatabase},
		{"mysqladmin drop mydb", DestructiveDatabase},
		{"psql -c \"ALTER TABLE t DROP COLUMN c\"", DestructiveDatabase},
		{"psql -c \"delete from t\"", DestructiveDatabase},
		{"cat f | psql -c \"DROP TABLE x\"", DestructiveDatabase},
		{"psql -c \"SELECT 1\"", ""},
		{"psql <<SQL\nSELECT 1;\nSQL", ""},
		{"grep \"DROP TABLE\" dump.sql", ""},

		// heredoc bodies and quoted text are data, not commands
		{"cat <<EOF\nrm -rf /\nEOF", ""},
		{"cat <<'EOF'\nrm -rf /\nEOF", ""},
		{"cat <<-EOF\n\trm -rf /\n\tEOF", ""},
		{"cat <<\"EOF\"\nrm -rf /\nEOF", ""},

		// cluster: resources destroyed through CLIs
		{"kubectl delete pod x", DestructiveCluster},
		{"kubectl -n prod delete deploy api", DestructiveCluster},
		{"kubectl drain node1", DestructiveCluster},
		{"kubectl rollout undo deploy/api", DestructiveCluster},
		{"kubectl replace -f x.yaml --force", DestructiveCluster},
		{"kubectl replace -f x.yaml", ""},
		{"kubectl get pods", ""},
		{"kubectl get pods; kubectl delete pod x", DestructiveCluster},
		{"kubectl describe pod x", ""},
		{"helm uninstall web", DestructiveCluster},
		{"helm rollback web 2", DestructiveCluster},
		{"helm list", ""},
		{"terraform destroy -auto-approve", DestructiveCluster},
		{"terraform apply -auto-approve", DestructiveCluster},
		{"terraform apply", ""},
		{"tofu destroy", DestructiveCluster},
		{"docker rm -f web", DestructiveCluster},
		{"docker rmi img", DestructiveCluster},
		{"docker system prune -a", DestructiveCluster},
		{"docker volume rm data", DestructiveCluster},
		{"docker volume prune", DestructiveCluster},
		{"docker compose down -v", DestructiveCluster},
		{"docker-compose down --volumes", DestructiveCluster},
		{"docker compose down", ""},
		{"docker ps", ""},
		{"aws s3 ls", ""},
		{"aws sts get-caller-identity", ""},
		{"aws ec2 terminate-instances --instance-ids i-1", DestructiveCluster},
		{"aws --profile dev ec2 terminate-instances", DestructiveCluster},
		{"aws cloudformation delete-stack --stack-name x", DestructiveCluster},
		{"gcloud compute instances delete vm1", DestructiveCluster},
		{"az group delete -n rg", DestructiveCluster},

		// empty and whitespace input
		{"", ""},
		{"   ", ""},
		{"ls -la", ""},
	}
	for _, c := range cases {
		kind, reason, ok := ClassifyDestructive(c.cmd)
		if c.want == "" {
			if ok {
				t.Errorf("ClassifyDestructive(%q) = %q (%q), want not destructive", c.cmd, kind, reason)
			}
			continue
		}
		if !ok || kind != c.want {
			t.Errorf("ClassifyDestructive(%q) = %q ok=%v, want %q", c.cmd, kind, ok, c.want)
		}
		if ok && strings.TrimSpace(reason) == "" {
			t.Errorf("ClassifyDestructive(%q) returned an empty reason", c.cmd)
		}
	}
}

func TestClassifyDestructiveReasonNamesTheOffendingCommand(t *testing.T) {
	_, reason, ok := ClassifyDestructive("ls && git push --force")
	if !ok || reason != "git push --force" {
		t.Fatalf("reason = %q ok=%v, want the offending simple command", reason, ok)
	}
}

func TestClassifyDestructiveFirstHitWins(t *testing.T) {
	kind, reason, ok := ClassifyDestructive("git push -f; rm -rf x")
	if !ok || kind != DestructiveGit || reason != "git push -f" {
		t.Fatalf("got %q %q %v, want the first destructive segment", kind, reason, ok)
	}
}

func TestClassifyDestructiveTrimsLongReasons(t *testing.T) {
	long := "rm -rf " + strings.Repeat("a", 200)
	_, reason, ok := ClassifyDestructive(long)
	if !ok {
		t.Fatal("expected a destructive command")
	}
	if len([]rune(reason)) > 83 {
		t.Fatalf("reason is %d runes, want about 80", len([]rune(reason)))
	}
}

// Read-only commands from readonly_test.go must never be destructive.
func TestReadOnlyCommandsAreNotDestructive(t *testing.T) {
	readOnly := []string{
		"ls -la", "git log | head", "git status && git diff", "rg foo | wc -l",
		"sed s/a/b/ f", "docker ps", "kubectl get pods -A", "kubectl get pods -n prod",
		"find . -name '*.go'", "awk '{print $1}' f", "pwd", "echo hi", "go version",
		"env", "git branch", "git branch -a",
	}
	for _, cmd := range readOnly {
		if ClassifyShell(cmd) != RiskRead {
			t.Fatalf("fixture %q is no longer read-only", cmd)
		}
		if kind, _, ok := ClassifyDestructive(cmd); ok {
			t.Errorf("read-only %q classified as destructive (%s)", cmd, kind)
		}
	}
}

func TestBashDestructiveForDecodesTheCommand(t *testing.T) {
	b := NewBash("")
	kind, reason, ok := b.DestructiveFor(mustJSON(t, map[string]string{"command": "git push --force"}))
	if !ok || kind != DestructiveGit || reason != "git push --force" {
		t.Fatalf("DestructiveFor = %q %q %v", kind, reason, ok)
	}
	if _, _, ok := b.DestructiveFor(mustJSON(t, map[string]string{"command": "ls"})); ok {
		t.Fatal("ls must not be destructive")
	}
	if _, _, ok := b.DestructiveFor("not json"); ok {
		t.Fatal("unreadable input must not be reported as destructive")
	}
}
