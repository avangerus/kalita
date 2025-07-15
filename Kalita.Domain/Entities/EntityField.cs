public class EntityField
{
    public Guid Id { get; set; }
    public Guid EntityTypeId { get; set; }
    public string Code { get; set; }
    public string Name { get; set; }
    public string FieldType { get; set; }
    public string? ReferenceTypeCode { get; set; }
    public bool IsRequired { get; set; }
    public bool IsCollection { get; set; }
    public Guid? ParentFieldId { get; set; }
}