package openapi2proto

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v2"
)

type namingConversionTestCase struct {
	Source   string
	Expected string
}

func TestNameConversions(t *testing.T) {
	t.Run("Endpoint", func(t *testing.T) {
		var tests = []namingConversionTestCase{
			{
				Source:   "/queue/{id}/enqueue_player",
				Expected: "GetQueueIdEnqueuePlayer",
			},
		}
		for _, test := range tests {
			t.Run(test.Source, func(t *testing.T) {
				// TODO: this seems like a misnomer
				if v := PathMethodToName(test.Source, "get", ""); v != test.Expected {
					t.Errorf("PathMethodToName conversion failed: expected %s, got %s", test.Expected, v)
				}
			})
		}
	})

	t.Run("Enum", func(t *testing.T) {
		var tests = []namingConversionTestCase{
			{
				Source:   "foo & bar",
				Expected: "FOO_AND_BAR",
			},
			{
				Source:   "foo&bar",
				Expected: "FOO_AND_BAR",
			},
			{
				Source: "bad chars % { } [ ] ( ) / . ' ’ -",
				// TODO: This seems like an invalid conversion. Check with authors later
				Expected: "BAD_CHARS____________",
			},
		}

		for _, test := range tests {
			t.Run(test.Source, func(t *testing.T) {
				if v := toEnum("test", test.Source, 0); v != test.Expected {
					t.Errorf("toEnum conversion failed: expected %s, got %s", test.Expected, v)
				}
			})
		}
	})
}

func TestRefType(t *testing.T) {
	tests := []struct {
		tName string
		ref   string
		defs  map[string]*Items

		want    string
		wantPkg string
	}{
		{
			"Simple ref",

			"#/definitions/Name",
			map[string]*Items{
				"Name": &Items{
					Type: "object",
				},
			},
			"Name",
			"",
		},
		{
			"URL nested ref",

			"http://something.com/commons/name.json#/definitions/Name",
			nil,
			"commons.name.Name",
			"commons/name.proto",
		},
		{
			"URL no ref",

			"http://something.com/commons/name.json",
			nil,
			"commons.Name",
			"commons/name.proto",
		},
		{
			"relative no ref",

			"commons/names/Name.json",
			nil,
			"commons.names.Name",
			"commons/names/name.proto",
		},
		{
			"relative nested ref",

			"commons/names/Name.json#/definitions/Name",
			nil,
			"commons.names.name.Name",
			"commons/names/name.proto",
		},
		{
			"relative nested ref",

			"something.json#/definitions/RelativeRef",
			nil,
			"something.RelativeRef",
			"something.proto",
		},

		{
			"relative nested ref",

			"names.json#/definitions/Name",
			nil,
			"names.Name",
			"names.proto",
		},

		{
			"relative ref, back one dir",

			"../commons/names/Name.json",
			nil,
			"commons.names.Name",
			"commons/names/name.proto",
		},
		{
			"relative nested ref, back two dir",

			"../../commons/names/Name.json#/definitions/Name",
			nil,
			"commons.names.name.Name",
			"commons/names/name.proto",
		},
	}

	for _, test := range tests {
		t.Run(test.tName, func(t *testing.T) {
			got, gotPkg := refType(test.ref, test.defs)
			if got != test.want {
				t.Errorf("[%s] expected %q got %q", test.tName, test.want, got)
			}

			if gotPkg != test.wantPkg {
				t.Errorf("[%s] expected package %q got %q", test.tName, test.wantPkg, gotPkg)
			}
		})
	}
}

type genProtoTestCase struct {
	options     bool
	fixturePath string
	wantProto   string
	remoteFiles []string
}

func testGenProto(t *testing.T, tests ...genProtoTestCase) {
	t.Helper()
	origin, _ := os.Getwd()
	for _, test := range tests {
		t.Run(test.fixturePath, func(t *testing.T) {
			for _, remoteFile := range test.remoteFiles {
				res, err := http.Get(remoteFile)
				if err != nil || res.StatusCode != http.StatusOK {
					t.Skip(`Remote file ` + remoteFile + ` is not available`)
				}
			}

			os.Chdir(origin)
			testSpec, err := ioutil.ReadFile(test.fixturePath)
			if err != nil {
				t.Fatal("unable to open test fixture: ", err)
			}

			os.Chdir(path.Dir(test.fixturePath))
			var testAPI APIDefinition
			if strings.HasSuffix(test.fixturePath, ".yaml") {
				err = yaml.Unmarshal(testSpec, &testAPI)
				if err != nil {
					t.Fatalf("unable to unmarshal text fixture into APIDefinition: %s - %s ",
						test.fixturePath, err)
				}
			} else {
				err = json.Unmarshal(testSpec, &testAPI)
				if err != nil {
					t.Fatalf("unable to unmarshal text fixture into APIDefinition: %s - %s",
						test.fixturePath, err)
				}

			}

			protoResult, err := GenerateProto(&testAPI, test.options)
			if err != nil {
				t.Fatal("unable to generate protobuf from APIDefinition: ", err)
			}

			os.Chdir(origin)
			// if test.wantProto is empty, guess file name from the original
			// fixture path
			wantProtoFile := test.wantProto
			if wantProtoFile == "" {
				i := strings.LastIndexByte(test.fixturePath, '.')
				if i > -1 {
					wantProtoFile = test.fixturePath[:i] + `.proto`
				} else {
					t.Fatalf(`unable to guess proto file name from %s`, test.fixturePath)
				}
			}
			want, err := ioutil.ReadFile(wantProtoFile)
			if err != nil {
				t.Fatal("unable to open test fixture: ", err)
			}

			if string(want) != string(protoResult) {
				diff := difflib.UnifiedDiff{
					A:        difflib.SplitLines(string(want)),
					B:        difflib.SplitLines(string(protoResult)),
					FromFile: wantProtoFile,
					ToFile:   "Generated",
					Context:  3,
				}
				text, _ := difflib.GetUnifiedDiffString(diff)
				t.Errorf("testYaml (%s) differences:\n%s",
					test.fixturePath, text)
			}
		})
	}
}

func TestNetwork(t *testing.T) {
	testGenProto(t, genProtoTestCase{
		fixturePath: "fixtures/petstore/swagger.yaml",
		remoteFiles: []string{
			"https://raw.githubusercontent.com/NYTimes/openapi2proto/master/fixtures/petstore/Pet.yaml",
		},
	})
}

func TestGenerateProto(t *testing.T) {
	tests := []genProtoTestCase{
		{
			fixturePath: "fixtures/cats.yaml",
		},
		{
			fixturePath: "fixtures/catsanddogs.yaml",
		},
		{
			fixturePath: "fixtures/semantic_api.json",
		},
		{
			fixturePath: "fixtures/most_popular.json",
		},
		{
			fixturePath: "fixtures/spec.yaml",
		},
		{
			fixturePath: "fixtures/spec.json",
		},
		{
			options:     true,
			fixturePath: "fixtures/semantic_api.json",
			wantProto:   "fixtures/semantic_api-options.proto",
		},
		{
			options:     true,
			fixturePath: "fixtures/most_popular.json",
			wantProto:   "fixtures/most_popular-options.proto",
		},
		{
			options:     true,
			fixturePath: "fixtures/spec.yaml",
			wantProto:   "fixtures/spec-options.proto",
		},
		{
			options:     true,
			fixturePath: "fixtures/spec.json",
			wantProto:   "fixtures/spec-options.proto",
		},
		{
			fixturePath: "fixtures/includes_query.json",
		},
		{
			fixturePath: "fixtures/lowercase_def.json",
		},
		{
			fixturePath: "fixtures/missing_type.json",
		},
		{
			fixturePath: "fixtures/kubernetes.json",
		},
		{
			fixturePath: "fixtures/accountv1-0.json",
		},
		{
			fixturePath: "fixtures/refs.json",
		},
		{
			fixturePath: "fixtures/refs.yaml",
		},
		{
			fixturePath: "fixtures/semantic_api.yaml",
		},
		{
			fixturePath: "fixtures/integers.yaml",
		},
		{
			fixturePath: "fixtures/global_options.yaml",
		},
		{
			fixturePath: "fixtures/naming_conversion.yaml",
		},
		{
			options: true,
			fixturePath: "fixtures/custom_options.yaml",
		},
	}
	testGenProto(t, tests...)
}
