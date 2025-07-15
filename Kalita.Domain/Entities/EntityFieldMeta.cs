public class EntityTypeMeta
{
    public string Code { get; set; } = "";
    public string Name { get; set; } = "";
    public List<EntityFieldMeta> Fields { get; set; } = new();
}