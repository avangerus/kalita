public class UpdateDictionaryItemRequest
{
    public string Value { get; set; }
    public string Code { get; set; }
    public string? ExtraJson { get; set; }
    public Guid? ParentId { get; set; }
}