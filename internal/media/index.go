package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gemmadesktop.local/app/internal/inference"
	"gemmadesktop.local/app/internal/store"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
)

func (m *Manager) Index(workspaceID, clipID string) (StartResult, error) {
	if e := m.scope(workspaceID); e != nil {
		return StartResult{}, e
	}
	clip, e := getDocument[Clip](m.db, "clips", workspaceID, clipID)
	if e != nil {
		return StartResult{}, e
	}
	if clip.SHA256 == "" || clip.DurationSeconds <= 0 {
		return StartResult{}, errors.New("import and validate the video before indexing")
	}
	var body string
	if e = m.db.QueryRow(`SELECT body FROM manifests WHERE clip_id=?`, clip.ID).Scan(&body); e != nil {
		return StartResult{}, e
	}
	var metadata manifest
	if e = json.Unmarshal([]byte(body), &metadata); e != nil {
		return StartResult{}, e
	}
	job := newJob(workspaceID, clip.ID, "index")
	return m.launch(job, func(tx *sql.Tx) error {
		clip.Status = "indexing"
		clip.Error = ""
		return putDocument(tx, "clips", clip.ID, clip.WorkspaceID, "", clip)
	}, func(ctx context.Context, j *Job, client *inference.Client) error {
		if client == nil {
			return errors.New("local embedding runtime is unavailable")
		}
		identity, e := client.ModelIdentity(ctx, m.config.EmbeddingModel, "embedding", "vision")
		if e != nil {
			return e
		}
		runtimeVersion, e := client.Version(ctx)
		if e != nil {
			return e
		}
		original, e := m.OpenMedia(workspaceID, clip.ID, "original", "")
		if e != nil {
			return e
		}
		input := original.File.Name()
		original.File.Close()
		indexID := store.ID()
		dir := filepath.Join(m.root, "indexes", indexID)
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		published := false
		defer func() {
			if !published {
				_ = os.RemoveAll(dir)
			}
		}()
		if e = m.progress(j, "extracting", 0, len(metadata.Samples)); e != nil {
			return e
		}
		extractor, e := extractorIdentity(ctx, "ffmpeg", input, dir)
		if e != nil {
			return e
		}
		paths, e := extract(ctx, input, dir, metadata)
		if e != nil {
			return e
		}
		if _, e = m.db.Exec(`INSERT INTO indexes(id,clip_id,model,digest,runtime,extractor,preprocessing,status) VALUES(?,?,?,?,?,?,?,'building')`, indexID, clip.ID, identity.Name, identity.Digest, runtimeVersion, extractor, preprocessing); e != nil {
			return e
		}
		defer func() {
			if !published {
				_, _ = m.db.Exec(`DELETE FROM frames WHERE index_id=?`, indexID)
				_, _ = m.db.Exec(`DELETE FROM indexes WHERE id=? AND status='building'`, indexID)
			}
		}()
		for i, path := range paths {
			if e = ctx.Err(); e != nil {
				return e
			}
			if e = m.progress(j, "embedding", i, len(paths)); e != nil {
				return e
			}
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			config, e := jpeg.DecodeConfig(f)
			if e != nil || config.Width < 1 || config.Height < 1 || config.Width > 768 || config.Height > 768 {
				f.Close()
				return errors.New("extractor produced an invalid bounded JPEG")
			}
			_, _ = f.Seek(0, io.SeekStart)
			bytes, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
			f.Close()
			if e != nil {
				return e
			}
			if len(bytes) > 1<<20 {
				return errors.New("extracted JPEG exceeds 1 MiB")
			}
			sum := sha256.Sum256(bytes)
			hash := hex.EncodeToString(sum[:])
			response, e := client.Embed(ctx, inference.EmbedRequest{Model: identity.Name, ExpectedDigest: identity.Digest, Input: []inference.EmbedInput{{Image: bytes}}})
			if e != nil {
				return e
			}
			if len(response.Embeddings) != 1 {
				return errors.New("embedding response does not match the sampled frame")
			}
			vector, e := vectorBytes(response.Embeddings[0])
			if e != nil {
				return e
			}
			relative, e := filepath.Rel(m.root, path)
			if e != nil {
				return e
			}
			sample := metadata.Samples[i]
			if _, e = m.db.Exec(`INSERT INTO frames(id,index_id,ordinal,pts,seconds,sha256,path,vector) VALUES(?,?,?,?,?,?,?,?)`, store.ID(), indexID, sample.Ordinal, sample.PTS, sample.TimeSeconds, hash, relative, vector); e != nil {
				return e
			}
			if e = os.Chmod(path, 0400); e != nil {
				return e
			}
		}
		check, e := m.OpenMedia(workspaceID, clip.ID, "original", "")
		if e != nil {
			return e
		}
		check.File.Close()
		afterExtractor, e := extractorIdentity(ctx, "ffmpeg", input, dir)
		if e != nil {
			return e
		}
		if afterExtractor != extractor {
			return errors.New("media extractor changed during indexing; index again")
		}
		afterRuntime, e := client.Version(ctx)
		if e != nil {
			return e
		}
		if afterRuntime != runtimeVersion {
			return errors.New("runtime changed during indexing; index again")
		}
		if e = m.publishIndex(ctx, clip, indexID, len(paths)); e != nil {
			return e
		}
		published = true
		return m.progress(j, "ready", len(paths), len(paths))
	})
}
