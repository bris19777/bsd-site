package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	folderMime        = "application/vnd.google-apps.folder"
	defaultAPIBase    = "https://www.googleapis.com/drive/v3"
	defaultUploadBase = "https://www.googleapis.com/upload/drive/v3"
	// Resumable chunks must be multiples of 256 KiB.
	defaultChunkSize = 32 * 256 << 10 // 8 MiB
	maxAttempts      = 6
)

// TokenSource yields OAuth access tokens. The agent's implementation asks
// the management server, so Google credentials never live on the client.
type TokenSource interface {
	Token(ctx context.Context, forceRefresh bool) (string, error)
}

type GDriveOptions struct {
	Tokens       TokenSource
	RootFolderID string // empty = "BSD Backup" folder in My Drive
	RepoName     string // per-device folder under the root
	HTTPClient   *http.Client
	// Overridable for tests.
	APIBase, UploadBase string
	ChunkSize           int
}

// GDrive stores the repository as a folder tree on Google Drive:
// <root>/<repo>/{config, blobs/, snapshots/}.
type GDrive struct {
	o       GDriveOptions
	repoID  string
	folders map[string]string // "blobs"/"snapshots" -> folder ID
	mu      sync.Mutex
	ids     map[string]string // key -> file ID cache
}

func OpenGDrive(ctx context.Context, o GDriveOptions) (*GDrive, error) {
	if o.Tokens == nil || o.RepoName == "" {
		return nil, errors.New("gdrive: token source and repo name are required")
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if o.APIBase == "" {
		o.APIBase = defaultAPIBase
	}
	if o.UploadBase == "" {
		o.UploadBase = defaultUploadBase
	}
	if o.ChunkSize <= 0 {
		o.ChunkSize = defaultChunkSize
	}
	g := &GDrive{o: o, folders: map[string]string{}, ids: map[string]string{}}
	root := o.RootFolderID
	if root == "" {
		var err error
		if root, err = g.ensureFolder(ctx, "root", "BSD Backup"); err != nil {
			return nil, err
		}
	}
	var err error
	if g.repoID, err = g.ensureFolder(ctx, root, o.RepoName); err != nil {
		return nil, err
	}
	for _, d := range []string{"blobs", "snapshots"} {
		if g.folders[d], err = g.ensureFolder(ctx, g.repoID, d); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func (g *GDrive) Describe() string { return "gdrive:" + g.repoID }

type driveFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size string `json:"size"`
}

func (f driveFile) size() int64 {
	n, _ := strconv.ParseInt(f.Size, 10, 64)
	return n
}

type driveList struct {
	NextPageToken string      `json:"nextPageToken"`
	Files         []driveFile `json:"files"`
}

func (g *GDrive) parent(key string) (parentID, name string, err error) {
	if !validKey(key) {
		return "", "", fmt.Errorf("gdrive: invalid key %q", key)
	}
	dir, name := splitKey(key)
	if dir == "" {
		return g.repoID, name, nil
	}
	return g.folders[dir], name, nil
}

func quoteQ(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

func (g *GDrive) find(ctx context.Context, parentID, name, mime string) (*driveFile, error) {
	q := "name = " + quoteQ(name) + " and " + quoteQ(parentID) + " in parents and trashed = false"
	if mime != "" {
		q += " and mimeType = " + quoteQ(mime)
	}
	files, err := g.query(ctx, q, 2)
	if err != nil || len(files) == 0 {
		return nil, err
	}
	return &files[0], nil
}

func (g *GDrive) query(ctx context.Context, q string, pageSize int) ([]driveFile, error) {
	var out []driveFile
	page := ""
	for {
		v := url.Values{
			"q": {q}, "fields": {"nextPageToken,files(id,name,size)"},
			"pageSize": {strconv.Itoa(pageSize)}, "corpora": {"allDrives"},
			"supportsAllDrives": {"true"}, "includeItemsFromAllDrives": {"true"},
		}
		if page != "" {
			v.Set("pageToken", page)
		}
		var list driveList
		if err := g.doJSON(ctx, http.MethodGet, g.o.APIBase+"/files?"+v.Encode(), nil, &list); err != nil {
			return nil, err
		}
		out = append(out, list.Files...)
		if list.NextPageToken == "" || len(out) >= pageSize && pageSize < 1000 {
			return out, nil
		}
		page = list.NextPageToken
	}
}

func (g *GDrive) ensureFolder(ctx context.Context, parentID, name string) (string, error) {
	f, err := g.find(ctx, parentID, name, folderMime)
	if err != nil {
		return "", err
	}
	if f != nil {
		return f.ID, nil
	}
	meta, _ := json.Marshal(map[string]any{"name": name, "mimeType": folderMime, "parents": []string{parentID}})
	var created driveFile
	err = g.doJSON(ctx, http.MethodPost, g.o.APIBase+"/files?supportsAllDrives=true&fields=id,name", meta, &created)
	return created.ID, err
}

func (g *GDrive) fileID(ctx context.Context, obj Object) (string, error) {
	if obj.ID != "" {
		return obj.ID, nil
	}
	g.mu.Lock()
	id, ok := g.ids[obj.Key]
	g.mu.Unlock()
	if ok {
		return id, nil
	}
	parentID, name, err := g.parent(obj.Key)
	if err != nil {
		return "", err
	}
	f, err := g.find(ctx, parentID, name, "")
	if err != nil {
		return "", err
	}
	if f == nil {
		return "", ErrNotExist
	}
	g.remember(obj.Key, f.ID)
	return f.ID, nil
}

func (g *GDrive) remember(key, id string) {
	g.mu.Lock()
	g.ids[key] = id
	g.mu.Unlock()
}

func (g *GDrive) Get(ctx context.Context, obj Object) (io.ReadCloser, error) {
	id, err := g.fileID(ctx, obj)
	if err != nil {
		return nil, err
	}
	resp, err := g.do(ctx, http.MethodGet, g.o.APIBase+"/files/"+url.PathEscape(id)+"?alt=media&supportsAllDrives=true", nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (g *GDrive) Delete(ctx context.Context, obj Object) error {
	id, err := g.fileID(ctx, obj)
	if errors.Is(err, ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	resp, err := g.do(ctx, http.MethodDelete, g.o.APIBase+"/files/"+url.PathEscape(id)+"?supportsAllDrives=true", nil, nil)
	switch {
	case err == nil:
		resp.Body.Close()
	case !errors.Is(err, ErrNotExist):
		return err
	}
	g.mu.Lock()
	delete(g.ids, obj.Key)
	g.mu.Unlock()
	return nil
}

func (g *GDrive) List(ctx context.Context, prefix string) ([]Object, error) {
	folder, ok := g.folders[prefix]
	if !ok {
		return nil, fmt.Errorf("gdrive: unknown prefix %q", prefix)
	}
	files, err := g.query(ctx, quoteQ(folder)+" in parents and trashed = false", 1000)
	if err != nil {
		return nil, err
	}
	out := make([]Object, 0, len(files))
	for _, f := range files {
		key := prefix + "/" + f.Name
		g.remember(key, f.ID)
		out = append(out, Object{Key: key, ID: f.ID, Size: f.size()})
	}
	return out, nil
}

// Put uses a resumable upload session with fixed-size chunks so large
// files survive network drops: a failed chunk is resumed from the offset
// Drive confirms, not restarted. Existing objects with the same key are
// replaced.
func (g *GDrive) Put(ctx context.Context, key string, r io.Reader) (Object, error) {
	parentID, name, err := g.parent(key)
	if err != nil {
		return Object{}, err
	}
	// Blob and snapshot keys are unique, so only config is ever replaced;
	// skip the lookup cost for everything else.
	var old string
	if key == "config" {
		old, _ = g.fileID(ctx, Object{Key: key})
	}

	meta, _ := json.Marshal(map[string]any{"name": name, "parents": []string{parentID}})
	resp, err := g.do(ctx, http.MethodPost,
		g.o.UploadBase+"/files?uploadType=resumable&supportsAllDrives=true&fields=id,name,size",
		meta, map[string]string{"X-Upload-Content-Type": "application/octet-stream"})
	if err != nil {
		return Object{}, err
	}
	resp.Body.Close()
	session := resp.Header.Get("Location")
	if session == "" {
		return Object{}, errors.New("gdrive: upload session has no Location")
	}

	br := bufio.NewReaderSize(r, g.o.ChunkSize)
	buf := make([]byte, g.o.ChunkSize)
	var offset int64
	for {
		n, rerr := io.ReadFull(br, buf)
		final := false
		switch {
		case rerr == io.EOF || rerr == io.ErrUnexpectedEOF:
			final = true
		case rerr != nil:
			return Object{}, rerr
		default:
			if _, perr := br.Peek(1); perr == io.EOF {
				final = true
			} else if perr != nil {
				return Object{}, perr
			}
		}
		f, err := g.sendChunk(ctx, session, buf[:n], offset, final)
		if err != nil {
			return Object{}, err
		}
		offset += int64(n)
		if final {
			if f == nil {
				return Object{}, errors.New("gdrive: upload finished without file metadata")
			}
			if old != "" && old != f.ID {
				g.Delete(ctx, Object{Key: key, ID: old})
			}
			g.remember(key, f.ID)
			return Object{Key: key, ID: f.ID, Size: offset}, nil
		}
	}
}

var (
	errSessionGone = errors.New("gdrive: upload session expired")
	errUploadFatal = errors.New("gdrive: upload rejected")
)

// sendChunk uploads chunk at offset, resuming after transient failures
// from whatever offset Drive reports it has committed.
func (g *GDrive) sendChunk(ctx context.Context, session string, chunk []byte, offset int64, final bool) (*driveFile, error) {
	total := "*"
	end := offset + int64(len(chunk))
	if final {
		total = strconv.FormatInt(end, 10)
	}
	committed := offset
	failures := 0
	for {
		part := chunk[committed-offset:]
		rng := "bytes */" + total
		if len(part) > 0 {
			rng = fmt.Sprintf("bytes %d-%d/%s", committed, end-1, total)
		}
		f, got, err := g.putRange(ctx, session, part, rng)
		if err == nil {
			switch {
			case f != nil:
				return f, nil
			case got >= end && !final:
				return nil, nil
			case got > committed:
				committed, failures = min(got, end), 0
				continue
			}
			err = errors.New("gdrive: upload made no progress")
		}
		if errors.Is(err, errSessionGone) || errors.Is(err, errUploadFatal) || ctx.Err() != nil {
			return nil, err
		}
		if failures++; failures > maxAttempts {
			return nil, err
		}
		if err := sleepBackoff(ctx, failures); err != nil {
			return nil, err
		}
		// Ask Drive how much it actually has before resending.
		if f, got, qerr := g.putRange(ctx, session, nil, "bytes */"+total); qerr == nil {
			if f != nil {
				return f, nil
			}
			if got >= offset {
				committed = min(got, end)
			}
		}
	}
}

// putRange PUTs one range. It returns the file on completion, otherwise
// the number of bytes Drive has committed (from the 308 Range header).
func (g *GDrive) putRange(ctx context.Context, session string, body []byte, contentRange string) (*driveFile, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, session, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Range", contentRange)
	tok, err := g.o.Tokens.Token(ctx, false)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := g.o.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		var f driveFile
		if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
			return nil, 0, err
		}
		return &f, 0, nil
	case resp.StatusCode == 308:
		rng := resp.Header.Get("Range") // "bytes=0-1234"
		if rng == "" {
			return nil, 0, nil
		}
		_, end, ok := strings.Cut(strings.TrimPrefix(rng, "bytes="), "-")
		n, perr := strconv.ParseInt(end, 10, 64)
		if !ok || perr != nil {
			return nil, 0, fmt.Errorf("gdrive: bad Range header %q", rng)
		}
		return nil, n + 1, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, 0, errSessionGone
	case resp.StatusCode == http.StatusUnauthorized:
		g.o.Tokens.Token(ctx, true)
		return nil, 0, apiError(resp)
	default:
		err := apiError(resp)
		if !retryable(resp.StatusCode, err) {
			return nil, 0, fmt.Errorf("%w: %w", errUploadFatal, err)
		}
		return nil, 0, err
	}
}

func (g *GDrive) doJSON(ctx context.Context, method, u string, body []byte, out any) error {
	resp, err := g.do(ctx, method, u, body, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// do sends a request with retries for 429/5xx and one token refresh on
// 401. The caller closes the body of a successful response.
func (g *GDrive) do(ctx context.Context, method, u string, body []byte, headers map[string]string) (*http.Response, error) {
	refreshed := false
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		tok, err := g.o.Tokens.Token(ctx, false)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := g.o.HTTPClient.Do(req)
		if err == nil {
			switch {
			case resp.StatusCode < 300:
				return resp, nil
			case resp.StatusCode == http.StatusNotFound:
				resp.Body.Close()
				return nil, ErrNotExist
			case resp.StatusCode == http.StatusUnauthorized && !refreshed:
				resp.Body.Close()
				refreshed = true
				if _, err := g.o.Tokens.Token(ctx, true); err != nil {
					return nil, err
				}
				continue
			}
			err = apiError(resp)
			if !retryable(resp.StatusCode, err) {
				return nil, err
			}
		}
		if ctx.Err() != nil || attempt >= maxAttempts {
			return nil, err
		}
		if err := sleepBackoff(ctx, attempt); err != nil {
			return nil, err
		}
	}
}

// retryable: 429, 5xx, and 403 only when Drive says it is a rate limit
// (403 otherwise means a real permission problem).
func retryable(status int, err error) bool {
	if status == http.StatusForbidden {
		return strings.Contains(err.Error(), "ateLimitExceeded")
	}
	return status == http.StatusTooManyRequests || status >= 500
}

func apiError(resp *http.Response) error {
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("gdrive: %s %s: %s", resp.Request.Method, resp.Status, strings.TrimSpace(string(b)))
}

func sleepBackoff(ctx context.Context, attempt int) error {
	d := time.Duration(1<<min(attempt, 6)) * 500 * time.Millisecond
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
