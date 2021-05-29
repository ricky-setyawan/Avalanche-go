// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/node"
)

func TestSetChainConfigs(t *testing.T) {
	tests := map[string]struct {
		configs      map[string]string
		upgrades     map[string]string
		corethConfig string
		expected     map[string]chains.ChainConfig
	}{
		"no chain configs": {
			configs:  map[string]string{},
			upgrades: map[string]string{},
			expected: map[string]chains.ChainConfig{},
		},
		"valid chain-id": {
			configs:  map[string]string{"yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp": "hello", "2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm": "world"},
			upgrades: map[string]string{"yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp": "helloUpgrades"},
			expected: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				id1, err := ids.FromString("yH8D7ThNJkxmtkuv2jgBa4P1Rn3Qpr4pPr7QYNfcdoS6k6HWp")
				assert.NoError(t, err)
				m[id1.String()] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("helloUpgrades")}

				id2, err := ids.FromString("2JVSBoinj9C2J33VntvzYtVJNZdN2NKiwwKjcumHUWEb5DbBrm")
				assert.NoError(t, err)
				m[id2.String()] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
		},
		"valid alias": {
			configs:  map[string]string{"C": "hello", "X": "world"},
			upgrades: map[string]string{"C": "upgradess"},
			expected: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				m["C"] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("upgradess")}
				m["X"] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
		},
		"coreth config only": {
			configs:      map[string]string{},
			upgrades:     map[string]string{},
			corethConfig: "hello",
			expected:     map[string]chains.ChainConfig{"C": {Config: []byte("hello"), Upgrade: []byte(nil)}},
		},
		"coreth with c alias chain config": {
			configs:      map[string]string{"C": "hello", "X": "world"},
			upgrades:     map[string]string{"C": "upgradess"},
			corethConfig: "hellocoreth",
			expected: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				m["C"] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("upgradess")}
				m["X"] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
		},
		"coreth with evm alias chain config": {
			configs:      map[string]string{"evm": "hello", "X": "world"},
			upgrades:     map[string]string{"evm": "upgradess"},
			corethConfig: "hellocoreth",
			expected: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				m["evm"] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("upgradess")}
				m["X"] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
		},
		"coreth and c chain upgrades in config": {
			configs:      map[string]string{"X": "world"},
			upgrades:     map[string]string{"C": "upgradess"},
			corethConfig: "hello",
			expected: func() map[string]chains.ChainConfig {
				m := map[string]chains.ChainConfig{}
				m["C"] = chains.ChainConfig{Config: []byte("hello"), Upgrade: []byte("upgradess")}
				m["X"] = chains.ChainConfig{Config: []byte("world"), Upgrade: []byte(nil)}

				return m
			}(),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			var configJSON string
			if len(test.corethConfig) > 0 {
				configJSON = fmt.Sprintf(`{%q: %q, %q: %q}`, ChainConfigDirKey, root, CorethConfigKey, test.corethConfig)
			} else {
				configJSON = fmt.Sprintf(`{%q: %q}`, ChainConfigDirKey, root)
			}
			configFile := setupConfigJSON(t, root, configJSON)
			chainsDir := path.Join(root, chainsSubDir)
			// Create custom configs
			for key, value := range test.configs {
				chainDir := path.Join(chainsDir, key)
				setupFile(t, chainDir, chainConfigFileName, value)
			}
			for key, value := range test.upgrades {
				chainDir := path.Join(chainsDir, key)
				setupFile(t, chainDir, chainUpgradeFileName, value)
			}

			v := setupViper(configFile)

			// Parse config
			assert.Equal(root, v.GetString(ChainConfigDirKey))
			var nodeConfig node.Config
			err := setChainConfigs(v, &nodeConfig)
			assert.NoError(err)
			assert.Equal(test.expected, nodeConfig.ChainConfigs)
		})
	}
}

func TestSetChainConfigsDirNotExist(t *testing.T) {
	tests := map[string]struct {
		structure string
		file      map[string]string
		expected  map[string]chains.ChainConfig
	}{
		"dir not exist": {
			structure: "/",
			file:      map[string]string{"C": "noeffect"},
			expected:  map[string]chains.ChainConfig{},
		},
		"chains dir not exist": {
			structure: "/cdir/",
			file:      map[string]string{"config": "noeffect"},
			expected:  map[string]chains.ChainConfig{},
		},
		"configs dir not exist": {
			structure: "/cdir/chains/",
			file:      map[string]string{"upgrade": "noeffect"},
			expected:  map[string]chains.ChainConfig{},
		},
		"full structure": {
			structure: "/cdir/chains/C/",
			file:      map[string]string{"config": "hello"},
			expected:  map[string]chains.ChainConfig{"C": {Config: []byte("hello"), Upgrade: []byte(nil)}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			root := t.TempDir()
			chainConfigDir := path.Join(root, "cdir")
			configJSON := fmt.Sprintf(`{%q: %q}`, ChainConfigDirKey, chainConfigDir)
			configFile := setupConfigJSON(t, root, configJSON)

			dirToCreate := path.Join(root, test.structure)
			assert.NoError(os.MkdirAll(dirToCreate, 0700))

			for key, value := range test.file {
				setupFile(t, dirToCreate, key, value)
			}
			v := setupViper(configFile)

			// Parse config
			assert.Equal(chainConfigDir, v.GetString(ChainConfigDirKey))
			// don't read with getConfigFromViper since it's very slow.
			nodeConfig := node.Config{}
			err := setChainConfigs(v, &nodeConfig)
			assert.NoError(err)
			assert.Equal(test.expected, nodeConfig.ChainConfigs)
		})
	}
}

func TestSetChainConfigDefaultDir(t *testing.T) {
	assert := assert.New(t)
	root := t.TempDir()
	// changes internal package variable, since using defaultDir (under user home) is risky.
	defaultChainConfigDir = path.Join(root, "configs")
	configFilePath := setupConfigJSON(t, root, "{}")

	v := setupViper(configFilePath)
	assert.Equal(defaultChainConfigDir, v.GetString(ChainConfigDirKey))

	chainsDir := path.Join(defaultChainConfigDir, "chains", "C")
	setupFile(t, chainsDir, "config", "helloworld")
	var nodeConfig node.Config
	err := setChainConfigs(v, &nodeConfig)
	assert.NoError(err)
	expected := map[string]chains.ChainConfig{"C": {Config: []byte("helloworld"), Upgrade: []byte(nil)}}
	assert.Equal(expected, nodeConfig.ChainConfigs)
}

// setups config json file and writes content
func setupConfigJSON(t *testing.T, rootPath string, value string) string {
	configFilePath := path.Join(rootPath, "config.json")
	assert.NoError(t, os.WriteFile(configFilePath, []byte(value), 0600))
	return configFilePath
}

// setups file creates necessary path and writes value to it.
func setupFile(t *testing.T, path string, fileName string, value string) {
	assert.NoError(t, os.MkdirAll(path, 0700))
	filePath := filepath.Join(path, fileName+".ex")
	assert.NoError(t, os.WriteFile(filePath, []byte(value), 0600))
}

func setupViper(configFilePath string) *viper.Viper {
	v := viper.New()
	fs := avalancheFlagSet()
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.PanicOnError) // flags are now reset
	pflag.CommandLine.AddGoFlagSet(fs)
	pflag.Parse()
	if err := v.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal(err)
	}
	// need to set it since in tests executable dir is somewhere /var/tmp/ (or wherever is designated by go)
	// thus it searches buildDir in /var/tmp/
	// but actual buildDir resides under project_root/build
	currentPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	v.Set(BuildDirKey, filepath.Join(currentPath, "..", "build"))
	v.SetConfigFile(configFilePath)
	err = v.ReadInConfig()
	if err != nil {
		log.Fatal(err)
	}
	return v
}
