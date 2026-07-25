package dockersave

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// writeFileT writes data to path, failing the test on error.
func writeFileT(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// extractTarFile reads a tar archive from disk into a name -> content map.
func extractTarFile(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening tar %s: %v", path, err)
	}
	defer f.Close()

	files := make(map[string][]byte)
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading tar entry %s: %v", hdr.Name, err)
		}
		files[hdr.Name] = data
	}
	return files
}

// TestAssembleDockerSaveTarAllowMissingBlobs is a regression test for a bug
// where --allow-missing-blobs was silently ignored for --format=tar: the
// missing-blob check was skipped, but every layer digest still reached
// ocitar's stream loop unconditionally, so the write failed with
// "blob <hex> not found" instead of succeeding with the layer omitted.
func TestAssembleDockerSaveTarAllowMissingBlobs(t *testing.T) {
	dir := t.TempDir()

	configData := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDigest := hashBytes(configData)
	configPath := filepath.Join(dir, "config.json")
	writeFileT(t, configPath, configData)

	layerData := []byte("app layer content")
	layerDigest := hashBytes(layerData)
	// Base layer is intentionally NOT written anywhere and NOT passed via
	// --layer: it simulates a shallow-pulled base image layer.
	baseLayerData := []byte("base layer content")
	baseLayerDigest := hashBytes(baseLayerData)

	manifest := v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config: v1.Descriptor{
			MediaType: types.OCIConfigJSON,
			Digest:    configDigest,
			Size:      int64(len(configData)),
		},
		Layers: []v1.Descriptor{
			{MediaType: types.OCILayer, Digest: baseLayerDigest, Size: int64(len(baseLayerData))},
			{MediaType: types.OCILayer, Digest: layerDigest, Size: int64(len(layerData))},
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshaling manifest: %v", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	writeFileT(t, manifestPath, manifestData)

	// Metadata + blob for only the app layer; the base layer has no --layer
	// entry at all, matching how rules_img invokes docker-save for a shallow
	// base image.
	layerMetaPath := filepath.Join(dir, "layer.meta.json")
	writeFileT(t, layerMetaPath, []byte(`{"digest":"sha256:`+layerDigest.Hex+`"}`))
	layerBlobPath := filepath.Join(dir, "layer.blob")
	writeFileT(t, layerBlobPath, layerData)

	outputPath := filepath.Join(dir, "out.tar")

	err = assembleDockerSave(
		manifestPath, configPath, outputPath, "tar",
		layerMappingFlag{{metadata: layerMetaPath, blob: layerBlobPath}},
		[]string{"repo:tag"}, []string{"repo:tag"},
		false, /* useSymlinks */
		true,  /* allowMissingBlobs */
		false, /* ociRefNameTagOnly */
	)
	if err != nil {
		t.Fatalf("assembleDockerSave with allowMissingBlobs failed: %v", err)
	}

	files := extractTarFile(t, outputPath)

	// The Docker manifest.json must still reference both layers: rewriting it
	// would change the content-addressed manifest digest.
	var dockerMfsts []DockerManifest
	if err := json.Unmarshal(files["manifest.json"], &dockerMfsts); err != nil {
		t.Fatalf("parsing manifest.json: %v", err)
	}
	wantLayers := []string{"blobs/sha256/" + baseLayerDigest.Hex, "blobs/sha256/" + layerDigest.Hex}
	if len(dockerMfsts[0].Layers) != len(wantLayers) {
		t.Fatalf("manifest.json layers = %v, want %v", dockerMfsts[0].Layers, wantLayers)
	}
	for _, want := range wantLayers {
		found := false
		for _, got := range dockerMfsts[0].Layers {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("manifest.json missing expected layer reference %s", want)
		}
	}

	// The base layer's bytes must not be embedded...
	if _, ok := files["blobs/sha256/"+baseLayerDigest.Hex]; ok {
		t.Error("base layer blob should not be embedded in the archive")
	}
	// ...but the app layer, which was available, must be.
	got, ok := files["blobs/sha256/"+layerDigest.Hex]
	if !ok {
		t.Fatal("app layer blob missing from archive")
	}
	if !bytes.Equal(got, layerData) {
		t.Error("app layer blob content mismatch")
	}
	if _, ok := files["blobs/sha256/"+configDigest.Hex]; !ok {
		t.Error("config blob missing from archive")
	}
}

// TestAssembleDockerSaveWithIndexTarAllowMissingBlobs is the multi-platform
// (image_index) counterpart of TestAssembleDockerSaveTarAllowMissingBlobs:
// runloop_container() builds an image_index, so this is the path that was
// actually broken for shallow base images.
func TestAssembleDockerSaveWithIndexTarAllowMissingBlobs(t *testing.T) {
	dir := t.TempDir()

	configData := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDigest := hashBytes(configData)
	configPath := filepath.Join(dir, "config.json")
	writeFileT(t, configPath, configData)

	layerData := []byte("app layer content")
	layerDigest := hashBytes(layerData)
	baseLayerData := []byte("base layer content")
	baseLayerDigest := hashBytes(baseLayerData)

	manifest := v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config: v1.Descriptor{
			MediaType: types.OCIConfigJSON,
			Digest:    configDigest,
			Size:      int64(len(configData)),
		},
		Layers: []v1.Descriptor{
			{MediaType: types.OCILayer, Digest: baseLayerDigest, Size: int64(len(baseLayerData))},
			{MediaType: types.OCILayer, Digest: layerDigest, Size: int64(len(layerData))},
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshaling manifest: %v", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	writeFileT(t, manifestPath, manifestData)

	manifestDigest := hashBytes(manifestData)
	index := v1.IndexManifest{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.index.v1+json",
		Manifests: []v1.Descriptor{{
			MediaType: types.OCIManifestSchema1,
			Digest:    manifestDigest,
			Size:      int64(len(manifestData)),
			Platform:  &v1.Platform{OS: "linux", Architecture: "amd64"},
		}},
	}
	indexData, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshaling index: %v", err)
	}
	indexPath := filepath.Join(dir, "index.json")
	writeFileT(t, indexPath, indexData)

	layerMetaPath := filepath.Join(dir, "layer.meta.json")
	writeFileT(t, layerMetaPath, []byte(`{"digest":"sha256:`+layerDigest.Hex+`"}`))
	layerBlobPath := filepath.Join(dir, "layer.blob")
	writeFileT(t, layerBlobPath, layerData)

	outputPath := filepath.Join(dir, "out.tar")

	err = assembleDockerSaveWithIndex(
		indexPath, outputPath, "tar",
		[]string{manifestPath}, []string{configPath},
		layerMappingFlag{{metadata: layerMetaPath, blob: layerBlobPath}},
		[]string{"repo:tag"}, []string{"repo:tag"},
		false, /* useSymlinks */
		true,  /* allowMissingBlobs */
		false, /* ociRefNameTagOnly */
	)
	if err != nil {
		t.Fatalf("assembleDockerSaveWithIndex with allowMissingBlobs failed: %v", err)
	}

	files := extractTarFile(t, outputPath)

	if _, ok := files["blobs/sha256/"+baseLayerDigest.Hex]; ok {
		t.Error("base layer blob should not be embedded in the archive")
	}
	got, ok := files["blobs/sha256/"+layerDigest.Hex]
	if !ok {
		t.Fatal("app layer blob missing from archive")
	}
	if !bytes.Equal(got, layerData) {
		t.Error("app layer blob content mismatch")
	}
	if _, ok := files["blobs/sha256/"+configDigest.Hex]; !ok {
		t.Error("config blob missing from archive")
	}
	indexDigest := hashBytes(indexData)
	if _, ok := files["blobs/sha256/"+indexDigest.Hex]; !ok {
		t.Error("index blob missing from archive")
	}

	var dockerMfsts []DockerManifest
	if err := json.Unmarshal(files["manifest.json"], &dockerMfsts); err != nil {
		t.Fatalf("parsing manifest.json: %v", err)
	}
	wantLayers := []string{"blobs/sha256/" + baseLayerDigest.Hex, "blobs/sha256/" + layerDigest.Hex}
	if len(dockerMfsts[0].Layers) != len(wantLayers) {
		t.Fatalf("manifest.json layers = %v, want %v", dockerMfsts[0].Layers, wantLayers)
	}
}

// TestAssembleDockerSaveTarMissingBlobsWithoutFlag confirms the ordinary
// error path (no --allow-missing-blobs) still fails before ever reaching
// ocitar, i.e. this fix did not weaken the default behavior.
func TestAssembleDockerSaveTarMissingBlobsWithoutFlag(t *testing.T) {
	dir := t.TempDir()

	configData := []byte(`{"architecture":"amd64","os":"linux"}`)
	configDigest := hashBytes(configData)
	configPath := filepath.Join(dir, "config.json")
	writeFileT(t, configPath, configData)

	baseLayerDigest := hashBytes([]byte("base layer content"))

	manifest := v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config: v1.Descriptor{
			MediaType: types.OCIConfigJSON,
			Digest:    configDigest,
			Size:      int64(len(configData)),
		},
		Layers: []v1.Descriptor{
			{MediaType: types.OCILayer, Digest: baseLayerDigest, Size: 19},
		},
	}
	manifestData, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(dir, "manifest.json")
	writeFileT(t, manifestPath, manifestData)

	outputPath := filepath.Join(dir, "out.tar")

	err := assembleDockerSave(
		manifestPath, configPath, outputPath, "tar",
		nil, []string{"repo:tag"}, []string{"repo:tag"},
		false, false, false,
	)
	if err == nil {
		t.Fatal("expected an error when a layer blob is missing and allowMissingBlobs is false")
	}
	if _, ok := err.(*MissingBlobsError); !ok {
		t.Errorf("expected *MissingBlobsError, got %T: %v", err, err)
	}
}
