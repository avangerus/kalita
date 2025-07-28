public class EntityFieldDto
{
    public string Code { get; set; }
    public string DisplayName { get; set; }
    public string Type { get; set; }
    public bool Required { get; set; }
    public int? MinLength { get; set; }
    public int? MaxLength { get; set; }
    public decimal? MinValue { get; set; }
    public decimal? MaxValue { get; set; }
    public string? Pattern { get; set; }
    public List<string>? AllowedValues { get; set; }
    public bool IsCollection { get; set; }
    public string? ReferenceTypeCode { get; set; }
    public string? DefaultValue { get; set; }
    public string? Description { get; set; }
    public List<string>? EnumOptions { get; set; }
    public List<string>? Values { get; set; }
}