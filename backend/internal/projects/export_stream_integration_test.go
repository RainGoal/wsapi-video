package projects

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type exportDownloadEvidence struct {
	bytes, sourceBytes int64
	heapGrowth         uint64
	allocated          uint64
	elapsed            time.Duration
}

func (f *exportFixture) waitDownload() {
	f.t.Helper()
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
		f.t.Fatal("download handler did not stop and close its streams")
	}
}

func (f *exportFixture) downloadFile(url string, size int64, digest string) exportDownloadEvidence {
	f.t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f.objects.peakHeap.Store(before.HeapAlloc)
	f.objects.readBytes.Store(0)
	started := time.Now()
	response, err := (&http.Client{Timeout: 30 * time.Second}).Get(url)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "application/zip" || !strings.Contains(response.Header.Get("Content-Disposition"), "attachment") {
		f.t.Fatal("native GET did not return an attachment", response.StatusCode, response.Header)
	}
	path := filepath.Join(f.t.TempDir(), "download.zip")
	output, err := os.Create(path)
	if err != nil {
		f.t.Fatal(err)
	}
	written, copyErr := io.Copy(output, response.Body)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		f.t.Fatal("streaming download failed", written, copyErr, closeErr)
	}
	f.waitDownload()
	runtime.ReadMemStats(&after)
	evidence := exportDownloadEvidence{bytes: written, sourceBytes: f.objects.readBytes.Load(), heapGrowth: f.objects.peakHeap.Load() - before.HeapAlloc, allocated: after.TotalAlloc - before.TotalAlloc, elapsed: time.Since(started)}
	archive, err := zip.OpenReader(path)
	if err != nil {
		f.t.Fatal("successful HTTP download is not a complete ZIP", err)
	}
	defer archive.Close()
	if len(archive.File) != 1 || archive.File[0].UncompressedSize64 != uint64(size) {
		f.t.Fatal("export duplicated or truncated an asset", archive.File)
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		f.t.Fatal(err)
	}
	defer entry.Close()
	hash := sha256.New()
	decoded, err := io.Copy(hash, entry)
	if err != nil || decoded != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		f.t.Fatal("ZIP CRC or complete asset SHA256 differs", decoded, err)
	}
	return evidence
}

func TestPostgresExportLargeNativeDownloadCancellationAndRetry(t *testing.T) {
	f := newExportFixture(t)
	const smallSize, largeSize int64 = 32 << 20, 256 << 20
	smallHash := f.seedObject("small-media", smallSize)
	largeHash := f.seedObject("large-media", largeSize)
	before := f.financialState()
	small := f.downloadFile(f.prepare("small-media"), smallSize, smallHash)
	// Both the signed snapshot and resulting ZIP must deduplicate repeated IDs.
	largeURL := f.prepare("large-media", "large-media")
	large := f.downloadFile(largeURL, largeSize, largeHash)
	if small.sourceBytes != smallSize || large.sourceBytes != largeSize || large.bytes <= largeSize || large.bytes > largeSize+4096 {
		t.Fatal("download did not stream the exact on-disk object", small, large)
	}
	if large.heapGrowth > 32<<20 || large.heapGrowth > small.heapGrowth+(16<<20) {
		t.Fatal("heap grew with the 8x media size", small, large)
	}
	f.objects.readBytes.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, largeURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err = io.CopyN(io.Discard, response.Body, 1<<20); err != nil {
		t.Fatal(err)
	}
	cancelledAt := time.Now()
	cancel()
	response.Body.Close()
	f.waitDownload()
	cancelElapsed := time.Since(cancelledAt)
	cancelledBytes := f.objects.readBytes.Load()
	if cancelledBytes >= largeSize || f.objects.opened.Load() != f.objects.closed.Load() {
		t.Fatal("cancelled download kept reading or retained an object stream", cancelledBytes, f.objects.opened.Load(), f.objects.closed.Load())
	}
	retry := f.downloadFile(largeURL, largeSize, largeHash)
	if after := f.financialState(); after != before {
		t.Fatal("native download/cancel/retry changed wallet, ledger, tasks, orders or asset count", before, after)
	}
	t.Logf("transport-only repeated media: small=%d ZIP bytes, source=%d, peak heap growth=%d, allocated=%d, duration=%s; large=%d ZIP bytes, source=%d, peak heap growth=%d, allocated=%d, duration=%s", small.bytes, small.sourceBytes, small.heapGrowth, small.allocated, small.elapsed, large.bytes, large.sourceBytes, large.heapGrowth, large.allocated, large.elapsed)
	t.Logf("cancel after 1MiB received: source stopped at %d bytes, streams closed within %s; same-link retry=%d bytes, same SHA256/CRC, no financial or task changes", cancelledBytes, cancelElapsed, retry.bytes)
}

func TestPostgresExportReadFailuresAbortHTTPAndZIP(t *testing.T) {
	f := newExportFixture(t)
	const size int64 = 64 << 20
	digest := f.seedObject("failure-media", size)
	f.seedObject("missing-media", 1024)
	before := f.financialState()
	for _, mode := range []string{"read_error", "short_eof", "overlong"} {
		t.Run(mode, func(t *testing.T) {
			f.objects.setMode(mode)
			if mode == "overlong" {
				if _, err := f.db.Exec(context.Background(), "UPDATE assets SET size_bytes=$1 WHERE id='failure-media'", int64(4<<20)); err != nil {
					t.Fatal(err)
				}
			}
			response, err := (&http.Client{Timeout: 10 * time.Second}).Get(f.prepare("failure-media"))
			if err != nil {
				t.Fatal("fixture must emit headers before the injected failure", err)
			}
			defer response.Body.Close()
			output, err := os.Create(filepath.Join(t.TempDir(), "failed.zip"))
			if err != nil {
				t.Fatal(err)
			}
			written, readErr := io.Copy(output, response.Body)
			output.Close()
			f.waitDownload()
			if readErr == nil || response.StatusCode != 200 || written == 0 || written >= size {
				t.Fatal("midstream failure silently completed HTTP", response.StatusCode, written, readErr)
			}
			if archive, err := zip.OpenReader(output.Name()); err == nil {
				archive.Close()
				t.Fatal("midstream failure produced a deceptively complete ZIP")
			}
			if f.objects.opened.Load() != f.objects.closed.Load() {
				t.Fatal("failed export retained an object stream")
			}
			t.Logf("headers=%d; received=%d; terminal read error=%v; ZIP rejected; all streams closed", response.StatusCode, written, readErr)
		})
	}
	f.objects.setMode("")
	if _, err := f.db.Exec(context.Background(), "UPDATE assets SET size_bytes=$1 WHERE id='failure-media'", size); err != nil {
		t.Fatal(err)
	}
	f.downloadFile(f.prepare("failure-media"), size, digest)
	missingURL := f.prepare("failure-media", "missing-media")
	var missingKey string
	if err := f.db.QueryRow(context.Background(), "SELECT object_key FROM assets WHERE id='missing-media'").Scan(&missingKey); err != nil {
		t.Fatal(err)
	}
	if err := f.objects.Delete(context.Background(), missingKey); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(missingURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, readErr := io.Copy(io.Discard, response.Body)
	f.waitDownload()
	if response.StatusCode != 503 || readErr != nil || strings.Contains(response.Header.Get("Content-Type"), "zip") || response.Header.Get("Content-Disposition") != "" || f.objects.opened.Load() != f.objects.closed.Load() {
		t.Fatal("missing object did not fail before ZIP headers", response.StatusCode, readErr, response.Header)
	}
	if after := f.financialState(); after != before {
		t.Fatal("failed/retried exports changed financial or asset records", before, after)
	}
}
