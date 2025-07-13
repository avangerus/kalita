public class DictionaryItem
{
    public Guid Id { get; set; }
    public Guid TypeId { get; set; }
    public string Code { get; set; } = "";
    public string Name { get; set; } = "";
    public string? Value { get; set; }
    public Guid? ParentId { get; set; }
}
