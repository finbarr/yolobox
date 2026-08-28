package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// customImageRepository is the repository every derived image is tagged under.
const customImageRepository = "yolobox-custom"

// customImageInspectFormat emits one tab-separated record per image: ID,
// creation time, size, project label, and comma-joined tags. The label lookup is
// guarded because images built before the label exists have no Labels map.
const customImageInspectFormat = "{{.Id}}\t{{.Created}}\t{{.Size}}\t" +
	"{{if .Config.Labels}}{{index .Config.Labels \"" + customImageProjectLabel + "\"}}{{end}}\t" +
	"{{join .RepoTags \",\"}}"

// customImage is a derived image in the yolobox-custom repository, as reported
// by the container runtime.
type customImage struct {
	ID      string
	Tags    []string
	Project string
	Created time.Time
	Size    int64
}

// imageInUse reports whether a container references the image. Containers
// record whichever reference they were started from, so both the image ID and
// its tags have to be checked.
func imageInUse(img customImage, inUse map[string]bool) bool {
	if inUse[img.ID] {
		return true
	}
	for _, tag := range img.Tags {
		if inUse[tag] {
			return true
		}
	}
	// `docker ps` abbreviates IDs to 12 characters and drops the algorithm
	// prefix, while `docker inspect` reports the full sha256: form. Treat a
	// reference that prefixes the image ID as a match, but only at full short-ID
	// length, so a stray short string cannot pin every image.
	bare := strings.TrimPrefix(img.ID, "sha256:")
	for ref := range inUse {
		ref = strings.TrimPrefix(ref, "sha256:")
		if len(ref) >= 12 && strings.HasPrefix(bare, ref) {
			return true
		}
	}
	return false
}

// classifyCustomImages sorts derived images into those to keep, those safe to
// delete, and those predating the project label. Images in use are always kept.
// Unlabeled images are never deleted: without a project they cannot be
// attributed, so there is no basis for calling one superseded.
func classifyCustomImages(images []customImage, inUse map[string]bool, dirExists func(string) bool) (keep, remove, unlabeled []customImage) {
	exists := make(map[string]bool)
	projectExists := func(project string) bool {
		if cached, ok := exists[project]; ok {
			return cached
		}
		found := dirExists(project)
		exists[project] = found
		return found
	}

	byProject := make(map[string][]customImage)
	for _, img := range images {
		switch {
		case imageInUse(img, inUse):
			keep = append(keep, img)
		case img.Project == "":
			unlabeled = append(unlabeled, img)
		default:
			byProject[img.Project] = append(byProject[img.Project], img)
		}
	}

	for project, group := range byProject {
		if !projectExists(project) {
			remove = append(remove, group...)
			continue
		}
		newest := group[0]
		for _, img := range group[1:] {
			if newer(img, newest) {
				newest = img
			}
		}
		for _, img := range group {
			if img.ID == newest.ID {
				keep = append(keep, img)
				continue
			}
			remove = append(remove, img)
		}
	}

	sortByID(keep)
	sortByID(remove)
	sortByID(unlabeled)
	return keep, remove, unlabeled
}

// newer breaks ties on Created by ID so classification is deterministic when
// two images share a timestamp.
func newer(candidate, current customImage) bool {
	if candidate.Created.Equal(current.Created) {
		return candidate.ID < current.ID
	}
	return candidate.Created.After(current.Created)
}

func sortByID(images []customImage) {
	sort.Slice(images, func(i, j int) bool { return images[i].ID < images[j].ID })
}

// listCustomImages returns every image in the yolobox-custom repository. The
// listing gives IDs only, so a second inspect call collects the metadata
// classification needs.
func listCustomImages(runtimePath string) ([]customImage, error) {
	output, err := exec.Command(runtimePath, "image", "ls", customImageRepository, "--format", "{{.ID}}").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list %s images: %w", customImageRepository, err)
	}
	var ids []string
	seen := make(map[string]bool)
	for _, id := range strings.Fields(string(output)) {
		if seen[id] {
			// An image carrying several tags is listed once per tag.
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	args := append([]string{"image", "inspect"}, ids...)
	args = append(args, "--format", customImageInspectFormat)
	output, err = exec.Command(runtimePath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect %s images: %w", customImageRepository, err)
	}

	var images []customImage
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		img, err := parseCustomImage(line)
		if err != nil {
			// One unreadable image must not abort the run, and must never be
			// deleted, so it is dropped from the classification entirely.
			warn("Skipping unreadable image record %q: %v", line, err)
			continue
		}
		images = append(images, img)
	}
	return images, nil
}

func parseCustomImage(line string) (customImage, error) {
	fields := strings.Split(line, "\t")
	if len(fields) < 5 {
		return customImage{}, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[1]))
	if err != nil {
		return customImage{}, fmt.Errorf("unparsable creation time %q", fields[1])
	}
	size, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
	if err != nil {
		return customImage{}, fmt.Errorf("unparsable size %q", fields[2])
	}
	var tags []string
	for _, tag := range strings.Split(fields[4], ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return customImage{
		ID:      strings.TrimSpace(fields[0]),
		Tags:    tags,
		Project: strings.TrimSpace(fields[3]),
		Created: created,
		Size:    size,
	}, nil
}

// containerImageRefs collects the image reference of every container, running or
// stopped. A failure here aborts the prune: without this set an in-use image
// could be deleted.
func containerImageRefs(runtimePath string) (map[string]bool, error) {
	output, err := exec.Command(runtimePath, "ps", "-a", "--format", "{{.Image}}").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	refs := make(map[string]bool)
	for _, ref := range strings.Fields(string(output)) {
		refs[ref] = true
	}
	return refs, nil
}

func isDir(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

// imageRef is the reference to delete an image by. Tags are preferred: deleting
// by ID fails when the image is referenced by a repository.
func imageRef(img customImage) string {
	if len(img.Tags) > 0 {
		return img.Tags[0]
	}
	return img.ID
}

func formatSize(bytes int64) string {
	const unit = 1000
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "kMGTPE"[exp])
}

func totalSize(images []customImage) int64 {
	var total int64
	for _, img := range images {
		total += img.Size
	}
	return total
}

func pruneCustomImages(args []string) error {
	fs := flag.NewFlagSet("prune-images", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "delete superseded images")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
			return errHelp
		}
		return err
	}

	cfg, err := loadConfigFromEnv()
	if err != nil {
		return err
	}
	runtimePath, err := resolveRuntime(cfg.Runtime)
	if err != nil {
		return err
	}

	images, err := listCustomImages(runtimePath)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		info("No %s images found; nothing to prune.", customImageRepository)
		return nil
	}
	inUse, err := containerImageRefs(runtimePath)
	if err != nil {
		return err
	}

	keep, remove, unlabeled := classifyCustomImages(images, inUse, isDir)
	reportCustomImages(keep, remove, unlabeled, inUse)

	if len(remove) == 0 {
		return nil
	}
	if !*force {
		info("Re-run with --force to delete the %d superseded image(s).", len(remove))
		return nil
	}

	var failed int
	for _, img := range remove {
		ref := imageRef(img)
		if err := exec.Command(runtimePath, "image", "rm", ref).Run(); err != nil {
			// Keep going: one image refusing to delete should not strand the rest.
			warn("Failed to remove %s: %v", ref, err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("failed to remove %d of %d image(s)", failed, len(remove))
	}
	success("Removed %d image(s), reclaiming %s.", len(remove), formatSize(totalSize(remove)))
	return nil
}

// reportCustomImages prints the classification grouped by project, so the dry
// run doubles as an inventory of which images belong to which project.
func reportCustomImages(keep, remove, unlabeled []customImage, inUse map[string]bool) {
	byProject := make(map[string][]customImage)
	for _, img := range append(append([]customImage{}, keep...), remove...) {
		if img.Project == "" {
			continue
		}
		byProject[img.Project] = append(byProject[img.Project], img)
	}
	removing := make(map[string]bool, len(remove))
	for _, img := range remove {
		removing[img.ID] = true
	}

	projects := make([]string, 0, len(byProject))
	for project := range byProject {
		projects = append(projects, project)
	}
	sort.Strings(projects)

	for _, project := range projects {
		status := project
		if !isDir(project) {
			status += " (missing)"
		}
		info("%s", status)
		group := byProject[project]
		sort.Slice(group, func(i, j int) bool { return group[i].Created.After(group[j].Created) })
		for _, img := range group {
			action, note := "keep  ", ""
			switch {
			case removing[img.ID]:
				action = "delete"
			case imageInUse(img, inUse):
				// Otherwise a superseded image marked "keep" looks arbitrary.
				note = "  (in use by a container)"
			}
			fmt.Fprintf(os.Stderr, "    %s  %-28s  %8s  %s%s\n",
				action, imageRef(img), formatSize(img.Size), img.Created.Format("2006-01-02"), note)
		}
	}

	if len(unlabeled) > 0 {
		info("Unlabeled images (built before yolobox recorded projects): %d, %s",
			len(unlabeled), formatSize(totalSize(unlabeled)))
		fmt.Fprintln(os.Stderr, "    These cannot be attributed to a project, so yolobox will not delete them.")
		fmt.Fprintln(os.Stderr, "    Review and remove them by hand with:")
		refs := make([]string, 0, len(unlabeled))
		for _, img := range unlabeled {
			refs = append(refs, imageRef(img))
		}
		fmt.Fprintf(os.Stderr, "      docker image rm %s\n", strings.Join(refs, " "))
	}

	if len(remove) == 0 {
		info("Nothing to prune.")
		return
	}
	info("%d image(s) superseded, %s reclaimable.", len(remove), formatSize(totalSize(remove)))
}
