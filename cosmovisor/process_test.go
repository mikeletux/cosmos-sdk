//go:build linux
// +build linux

package cosmovisor_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/cosmovisor"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

type processTestSuite struct {
	suite.Suite
}

func TestProcessTestSuite(t *testing.T) {
	suite.Run(t, new(processTestSuite))
}

// TestLaunchProcess will try running the script a few times and watch upgrades work properly
// and args are passed through
func (s *processTestSuite) TestLaunchProcess() {
	// binaries from testdata/validate directory
	require := s.Require()
	home := copyTestData(s.T(), "validate")
	cfg := &cosmovisor.Config{Home: home, Name: "dummyd", PollInterval: 20, UnsafeSkipBackup: true}

	// should run the genesis binary and produce expected output
	var stdout, stderr = NewBuffer(), NewBuffer()
	currentBin, err := cfg.CurrentBin()
	require.NoError(err)
	require.Equal(cfg.GenesisBin(), currentBin)

	launcher, err := cosmovisor.NewLauncher(cfg)
	require.NoError(err)

	upgradeFile := cfg.UpgradeInfoFilePath()
	args := []string{"foo", "bar", "1234", upgradeFile}
	doUpgrade, err := launcher.Run(args, stdout, stderr)
	require.NoError(err)
	require.True(doUpgrade)
	require.Equal("", stderr.String())
	require.Equal(fmt.Sprintf("Genesis foo bar 1234 %s\nUPGRADE \"chain2\" NEEDED at height: 49: {}\n", upgradeFile),
		stdout.String())

	// ensure this is upgraded now and produces new output

	currentBin, err = cfg.CurrentBin()
	require.NoError(err)

	require.Equal(cfg.UpgradeBin("chain2"), currentBin)
	args = []string{"second", "run", "--verbose"}
	stdout.Reset()
	stderr.Reset()

	doUpgrade, err = launcher.Run(args, stdout, stderr)
	require.NoError(err)
	require.False(doUpgrade)
	require.Equal("", stderr.String())
	require.Equal("Chain 2 is live!\nArgs: second run --verbose\nFinished successfully\n", stdout.String())

	// ended without other upgrade
	require.Equal(cfg.UpgradeBin("chain2"), currentBin)
}

// TestLaunchProcess will try running the script a few times and watch upgrades work properly
// and args are passed through
func (s *processTestSuite) TestLaunchProcessWithDownloads() {
	// test case upgrade path (binaries from testdata/download directory):
	// genesis -> chain2-zip_bin
	// chain2-zip_bin -> ref_to_chain3-zip_dir.json = (json for the next download instructions) -> chain3-zip_dir
	// chain3-zip_dir - doesn't upgrade
	require := s.Require()
	home := copyTestData(s.T(), "download")
	cfg := &cosmovisor.Config{Home: home, Name: "autod", AllowDownloadBinaries: true, PollInterval: 100, UnsafeSkipBackup: true}
	upgradeFilename := cfg.UpgradeInfoFilePath()

	// should run the genesis binary and produce expected output
	currentBin, err := cfg.CurrentBin()
	require.NoError(err)
	require.Equal(cfg.GenesisBin(), currentBin)

	launcher, err := cosmovisor.NewLauncher(cfg)
	require.NoError(err)

	var stdout, stderr = NewBuffer(), NewBuffer()
	args := []string{"some", "args", upgradeFilename}
	doUpgrade, err := launcher.Run(args, stdout, stderr)

	require.NoError(err)
	require.True(doUpgrade)
	require.Equal("", stderr.String())
	require.Equal("Genesis autod. Args: some args "+upgradeFilename+"\n"+`ERROR: UPGRADE "chain2" NEEDED at height: 49: zip_binary`+"\n", stdout.String())
	currentBin, err = cfg.CurrentBin()
	require.NoError(err)
	require.Equal(cfg.UpgradeBin("chain2"), currentBin)

	// start chain2
	stdout.Reset()
	stderr.Reset()
	args = []string{"run", "--fast", upgradeFilename}
	doUpgrade, err = launcher.Run(args, stdout, stderr)
	require.NoError(err)

	require.Equal("", stderr.String())
	require.Equal("Chain 2 from zipped binary\nArgs: run --fast "+upgradeFilename+"\n"+`ERROR: UPGRADE "chain3" NEEDED at height: 936: ref_to_chain3-zip_dir.json module=main`+"\n", stdout.String())
	// ended with one more upgrade
	require.True(doUpgrade)
	currentBin, err = cfg.CurrentBin()
	require.NoError(err)
	require.Equal(cfg.UpgradeBin("chain3"), currentBin)

	// run the last chain
	args = []string{"end", "--halt", upgradeFilename}
	stdout.Reset()
	stderr.Reset()
	doUpgrade, err = launcher.Run(args, stdout, stderr)
	require.NoError(err)
	require.False(doUpgrade)
	require.Equal("", stderr.String())
	require.Equal("Chain 3 from zipped directory\nArgs: end --halt "+upgradeFilename+"\n", stdout.String())

	// and this doesn't upgrade
	currentBin, err = cfg.CurrentBin()
	require.NoError(err)
	require.Equal(cfg.UpgradeBin("chain3"), currentBin)
}

// TestSkipUpgrade tests heights that are identified to be skipped and return if upgrade height matches the skip heights
func TestSkipUpgrade(t *testing.T) {
	cases := []struct {
		args        []string
		upgradeInfo upgradetypes.Plan
		expectRes   bool
	}{{
		args:        []string{"appb", "start", "--unsafe-skip-upgrades"},
		upgradeInfo: upgradetypes.Plan{Name: "upgrade1", Info: "some info", Height: 123},
		expectRes:   false,
	}, {
		args:        []string{"appb", "start", "--unsafe-skip-upgrades", "--abcd"},
		upgradeInfo: upgradetypes.Plan{Name: "upgrade1", Info: "some info", Height: 123},
		expectRes:   false,
	}, {
		args:        []string{"appb", "start", "--unsafe-skip-upgrades", "10", "--abcd"},
		upgradeInfo: upgradetypes.Plan{Name: "upgrade1", Info: "some info", Height: 11},
		expectRes:   false,
	}, {
		args:        []string{"appb", "start", "--unsafe-skip-upgrades", "10", "20", "--abcd"},
		upgradeInfo: upgradetypes.Plan{Name: "upgrade1", Info: "some info", Height: 20},
		expectRes:   true,
	}, {
		args:        []string{"appb", "start", "--unsafe-skip-upgrades", "10", "20", "--abcd", "34"},
		upgradeInfo: upgradetypes.Plan{Name: "upgrade1", Info: "some info", Height: 34},
		expectRes:   false,
	}}

	for i := range cases {
		tc := cases[i]
		require := require.New(t)
		h := cosmovisor.IsSkipUpgradeHeight(tc.args, tc.upgradeInfo)
		require.Equal(h, tc.expectRes)
	}
}

// TestUpgradeSkipHeights tests if correct skip upgrade heights are identified from the cli args
func TestUpgradeSkipHeights(t *testing.T) {
	cases := []struct {
		args      []string
		expectRes []int
	}{{
		args:      []string{},
		expectRes: nil,
	}, {
		args:      []string{"appb", "start"},
		expectRes: nil,
	}, {
		args:      []string{"appb", "start", "--unsafe-skip-upgrades"},
		expectRes: nil,
	}, {
		args:      []string{"appb", "start", "--unsafe-skip-upgrades", "--abcd"},
		expectRes: nil,
	}, {
		args:      []string{"appb", "start", "--unsafe-skip-upgrades", "10", "--abcd"},
		expectRes: []int{10},
	}, {
		args:      []string{"appb", "start", "--unsafe-skip-upgrades", "10", "20", "--abcd"},
		expectRes: []int{10, 20},
	}, {
		args:      []string{"appb", "start", "--unsafe-skip-upgrades", "10", "20", "--abcd", "34"},
		expectRes: []int{10, 20},
	}, {
		args:      []string{"appb", "start", "--unsafe-skip-upgrades", "10", "as", "20", "--abcd"},
		expectRes: []int{10, 20},
	}}

	for i := range cases {
		tc := cases[i]
		require := require.New(t)
		h := cosmovisor.UpgradeSkipHeights(tc.args)
		require.Equal(h, tc.expectRes)
	}
}

func TestBackupScript(t *testing.T) {
	// Create test scripts
	workingTestScript := `#!/bin/sh
exit 0`
	failingTestScript := `#!/bin/sh
exit 1`

	testCases := []struct {
		ScriptPath    string // Path where the script is in the computer
		ScriptContent string // Content of the script in case we want to write it in disk
		WriteScript   bool   // True if we want to write the script to disk
		HasError      bool   // If the testcase is expected to have error
	}{
		{
			ScriptPath:    "/var/tmp/workingTestScript.sh",
			ScriptContent: workingTestScript,
			WriteScript:   true,
			HasError:      false,
		},
		{
			ScriptPath:    "/var/tmp/failingTestScript.sh",
			ScriptContent: failingTestScript,
			WriteScript:   true,
			HasError:      true,
		},
		{
			ScriptPath:  "/var/tmp/phantomTestScript.sh",
			WriteScript: false,
			HasError:    true,
		},
	}

	for _, testCase := range testCases {
		if testCase.WriteScript {
			err := ioutil.WriteFile(testCase.ScriptPath, []byte(testCase.ScriptContent), 0755)
			if err != nil {
				t.Errorf("the was an error when creating the script testing file - %s", err)
			}
			defer os.Remove(testCase.ScriptPath) // Remove the testing script when the function returns
		}

		config := &cosmovisor.Config{
			UnsafeSkipBackup:  false,
			ScriptBackupShell: "/bin/sh",
			ScriptBackupPath:  testCase.ScriptPath,
		}

		err := cosmovisor.DoBackup(config)
		if err != nil && !testCase.HasError {
			t.Errorf("Expected no error but got one - %s", err)
		}
		if err == nil && testCase.HasError {
			t.Errorf("expected error but got no one")
		}
	}
}
