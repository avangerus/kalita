package reference

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadEnumCatalog читает все enum-справочники из папки reference/enums/
func LoadEnumCatalog(dir string) (map[string]EnumDirectory, error) {
	result := make(map[string]EnumDirectory)
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if !file.IsDir() && (strings.HasSuffix(file.Name(), ".yaml") || strings.HasSuffix(file.Name(), ".yml")) {
			path := filepath.Join(dir, file.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var enumDir EnumDirectory
			if err := yaml.Unmarshal(data, &enumDir); err != nil {
				return nil, err
			}
			// Имя справочника — из enumDir.Name или из имени файла
			enumName := enumDir.Name
			if enumName == "" {
				enumName = strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
			}
			result[enumName] = enumDir
		}
	}
	return result, nil
}
