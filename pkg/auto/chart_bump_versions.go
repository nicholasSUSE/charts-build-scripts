package auto

import (
	"strings"

	"github.com/blang/semver"
)

type versions struct {
	latest              *version
	latestRepoPrefix    *version
	toRelease           *version
	toReleaseRepoPrefix *version
}

type version struct {
	txt string
	svr *semver.Version
}

func (v *version) updateSemver() error {
	newSemver, err := semver.Make(v.txt)
	if err != nil {
		return err
	}
	v.svr = &newSemver
	return nil
}

func (v *version) updateTxt() {
	v.txt = v.svr.String()
}

// calculateNextVersion will calculate the next version to bump based on the latest version
// if the chart had a patch bump, it will increment the patch version for the repoPrefixVersion
// if the chart had a minor or major bump, it will increment the minor version for the repoPrefixVersion
// the major repoPrefixVersion is only bumped when Rancher version is bumped.
func (b *Bump) calculateNextVersion() error {
	// load versions and parse the repository prefix versions from them
	if err := b.loadVersions(); err != nil {
		return err
	}

	// check and parse the versions before building the new version
	if err := b.applyVersionRules(); err != nil {
		return err
	}

	// build: toRelease full version
	targetVersion := b.versions.toReleaseRepoPrefix.txt + "+up" + b.versions.toRelease.txt
	targetSemver := semver.MustParse(targetVersion)
	b.releaseYaml.ChartVersion = targetVersion
	b.Pkg.AutoGeneratedBumpVersion = &targetSemver

	return nil
}

// loadVersions will load the latest version from the index.yaml and the version to release from the chart owner upstream repository
// rules:
//   - latest version may/may not contain a repoPrefixVersion
//   - to release version must not contain a repoPrefixVersion
func (b *Bump) loadVersions() error {
	b.versions = &versions{
		latest:              &version{},
		latestRepoPrefix:    &version{},
		toRelease:           &version{},
		toReleaseRepoPrefix: &version{},
	}

	// latestVersion and latestRepoPrefixVersion are the latest versions from the index.yaml
	// get the latest released version from the index.yaml (the first version is the latest; already sorted)
	latestUnparsedVersion := b.assetsVersionsMap[b.targetChart][0].Version
	if latestUnparsedVersion == "" {
		return errChartLatestVersion
	}

	// Latest version may/may not contain a repoPrefixVersion
	latestRepoPrefix, latestVersion, found := parseRepoPrefixVersionIfAny(latestUnparsedVersion)
	if found {
		b.versions.latestRepoPrefix.txt = latestRepoPrefix
		if err := b.versions.latestRepoPrefix.updateSemver(); err != nil {
			return err
		}
	}
	b.versions.latest.txt = latestVersion
	if err := b.versions.latest.updateSemver(); err != nil {
		return err
	}

	// toRelease version comes from the chart owner upstream repository
	b.versions.toRelease.txt = b.Pkg.Chart.GetUpstreamVersion()
	if b.versions.toRelease.txt == "" {
		return errChartUpstreamVersion
	}
	if err := b.versions.toRelease.updateSemver(); err != nil {
		return err
	}

	// upstream/(to release version) must not contain a repoPrefixVersion
	_, _, found = parseRepoPrefixVersionIfAny(b.versions.toRelease.txt)
	if found {
		return errChartUpstreamVersionWrong
	}

	// Check if latestVersion > versionToRelease before continuing
	if b.versions.toRelease.svr.LT(*b.versions.latest.svr) {
		return errBumpVersion
	}

	return nil
}

// parseRepoPrefixVersionIfAny will parse the repository prefix version if it exists
func parseRepoPrefixVersionIfAny(unparsedVersion string) (repoPrefix, version string, found bool) {
	found = strings.Contains(unparsedVersion, "+up")
	if found {
		versions := strings.Split(unparsedVersion, "+up")
		repoPrefix = versions[0]
		version = versions[1]
	} else {
		version = unparsedVersion
	}

	return repoPrefix, version, found
}

func (b *Bump) applyVersionRules() error {

	// get the repository major prefix version rule (i.e., 105; 104; 103...)
	repoPrefixVersionRule := b.versionRules.Rules[b.versionRules.BranchVersion].Min
	repoPrefixSemverRule, err := semver.Make(repoPrefixVersionRule)
	if err != nil {
		return err
	}

	/** This will handle the cases:
	* 	- last version: X.Y.Z | repoPrefixVersion: 105.0.0
	*   - last version: 104.X.Y+upX.Y.Z | repoPrefixVersion: 105.0.0
	* in each case, the repoPrefixVersion will be bumped to 105.0.0
	 */
	if b.versions.latestRepoPrefix.txt == "" || repoPrefixSemverRule.Major != b.versions.latestRepoPrefix.svr.Major {
		b.versions.toReleaseRepoPrefix.txt = repoPrefixVersionRule
		if err := b.versions.toReleaseRepoPrefix.updateSemver(); err != nil {
			return err
		}
		// if we are changing branch lines the repoPrefix will always be: 10X.0.0; return now.
		return nil
	}

	b.versions.toReleaseRepoPrefix.txt = b.versions.latestRepoPrefix.txt
	if err := b.versions.toReleaseRepoPrefix.updateSemver(); err != nil {
		return err
	}

	// now only calculate if it is a minor or patch bump according to the latest version.
	majorBump := b.versions.toRelease.svr.Major > b.versions.latest.svr.Major
	minorBump := b.versions.toRelease.svr.Minor > b.versions.latest.svr.Minor
	patchBump := b.versions.toRelease.svr.Patch > b.versions.latest.svr.Patch

	if patchBump && !majorBump && !minorBump {
		b.versions.toReleaseRepoPrefix.svr.Patch++ // patch bump
	}
	if minorBump || majorBump {
		b.versions.toReleaseRepoPrefix.svr.Minor++ // minor bump
		b.versions.toReleaseRepoPrefix.svr.Patch = 0
	}

	b.versions.toReleaseRepoPrefix.updateTxt()
	return nil
}
