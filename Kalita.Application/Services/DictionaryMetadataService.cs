using System.Text.Json;

public class DictionaryMetadataService
{
    private readonly string _configPath;
    private List<DictionaryTypeMeta>? _types;

    public DictionaryMetadataService(string configPath)
    {
        _configPath = configPath;
        Load();
    }

    public void Load()
    {
        var json = File.ReadAllText(_configPath);
        _types = JsonSerializer.Deserialize<List<DictionaryTypeMeta>>(json, new JsonSerializerOptions { PropertyNameCaseInsensitive = true });
    }

    public List<DictionaryTypeMeta> GetTypes() => _types ?? new List<DictionaryTypeMeta>();
    public DictionaryTypeMeta? GetTypeByCode(string code) => _types?.FirstOrDefault(t => t.Code == code);
}

// DTO для структуры meta
public class DictionaryTypeMeta
{
    public string Code { get; set; } = "";
    public string Name { get; set; } = "";
    public List<DictionaryFieldMeta> Fields { get; set; } = new();
}
public class DictionaryFieldMeta
{
    public string Name { get; set; } = "";
    public string Type { get; set; } = "";
    public bool Required { get; set; }
}
