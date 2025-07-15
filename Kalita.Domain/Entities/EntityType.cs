public class EntityType
{
    public Guid Id { get; set; }
    public string Code { get; set; }
    public string Name { get; set; }
    public string? Description { get; set; }
    public List<EntityField> Fields { get; set; } = new();
}