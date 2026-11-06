package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

func TestPublicPointerCoveragePreservesOriginalContractAndScopesReferenceFork(t *testing.T) {
	packs, err := LoadFixturePacks()
	if err != nil {
		t.Fatal(err)
	}
	findPack := func(id string) FixturePack {
		for _, p := range packs {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("missing fixture pack %s", id)
		return FixturePack{}
	}
	findTask := func(p FixturePack) Task {
		for _, task := range p.Tasks {
			if task.ID == "json-pointer" {
				return task
			}
		}
		t.Fatal("missing JSON pointer task")
		return Task{}
	}
	original := findPack("coding-diagnostics-v2")
	sibling := findPack("coding-diagnostics-v2-public-coverage")
	if original.Version != "2.1.0" || sibling.Version != "2.2.0" || original.GraderPolicy != "task-contract-v3" || sibling.GraderPolicy != original.GraderPolicy {
		t.Fatal("coverage update changed the original version or grading policy")
	}
	if len(sibling.Tasks) != 1 || sibling.Digest == original.Digest {
		t.Fatal("sibling requires a separate one-task pack identity")
	}
	oldTask, newTask := findTask(original), findTask(sibling)
	if oldTask.Split != "development" || newTask.Split != oldTask.Split || oldTask.ExposureFamily != newTask.ExposureFamily || oldTask.PublicHash != newTask.PublicHash {
		t.Fatal("public coverage must retain development exposure and public identity")
	}
	if len(newTask.Cases) != len(oldTask.Cases)+8 || !reflect.DeepEqual(newTask.Cases[:len(oldTask.Cases)], oldTask.Cases) {
		t.Fatal("coverage update must append eight cases without altering inherited cases")
	}
	if newTask.GraderHash == oldTask.GraderHash {
		t.Fatal("added cases require a new grading identity")
	}
	oldTask.Cases, newTask.Cases = nil, nil
	oldTask.GraderHash, newTask.GraderHash = "", ""
	if !reflect.DeepEqual(oldTask, newTask) {
		t.Fatal("sibling changed public files, prompt, authority or other task metadata")
	}
	oldRef, err := packFiles.ReadFile("packs/coding-diagnostics-v2/json-pointer/reference.json")
	if err != nil {
		t.Fatal(err)
	}
	newRef, err := packFiles.ReadFile("packs/coding-diagnostics-v2-public-coverage/json-pointer/reference.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		bytes []byte
		want  string
	}{
		{oldRef, "622cd6e9e88a23ef672f16076ec33856335abc3719f47041b49d50e4e4dbfde6"},
		{newRef, "3df8ea31a61fb249aac15854cc151f202d9445404f42600e39221b0768c90a69"},
	} {
		sum := sha256.Sum256(item.bytes)
		if hex.EncodeToString(sum[:]) != item.want {
			t.Fatal("reference source identity changed")
		}
	}
	var oldFiles, newFiles map[string]string
	if json.Unmarshal(oldRef, &oldFiles) != nil || json.Unmarshal(newRef, &newFiles) != nil || len(oldFiles) != len(newFiles) {
		t.Fatal("reference fork changed its file inventory")
	}
	for name, body := range oldFiles {
		if name != "logic.go" && newFiles[name] != body {
			t.Fatal("reference fork changed an unrelated file")
		}
	}
	if oldFiles["logic.go"] == newFiles["logic.go"] {
		t.Fatal("sibling reference did not receive its scoped correction")
	}
}
