package project

import "fmt"

func AssetName(md5ext, assetID, format string) string {
	if md5ext != "" {
		return md5ext
	}
	if assetID == "" || format == "" {
		return ""
	}
	return assetID + "." + format
}

// ReadAssets reads only referenced files, never extracting archive paths.
func ReadAssets(path string, p *Project) (map[string][]byte, error) {
	reader, err := OpenAssets(path, p)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	// Unlike OpenAssets, this function retains every asset in memory.
	var total int64
	for _, entry := range reader.assets {
		if entry.size > maxAssetsSize-total {
			return nil, fmt.Errorf("assets exceed archive memory limit")
		}
		total += entry.size
	}
	assets := make(map[string][]byte, len(reader.assets))
	for name := range reader.assets {
		data, err := reader.Read(name)
		if err != nil {
			return nil, err
		}
		assets[name] = data
	}
	return assets, nil
}
