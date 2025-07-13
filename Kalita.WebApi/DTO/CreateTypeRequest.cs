public class CreateTypeRequest
{
    public string Code { get; set; } = "";
    public string Name { get; set; } = "";
}
public class CreateItemRequest
{
    public string Code { get; set; } = "";
    public string Name { get; set; } = "";
    public string? Value { get; set; }
    public Guid? ParentId { get; set; }
}
