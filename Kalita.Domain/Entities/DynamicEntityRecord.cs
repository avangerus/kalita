public class DynamicEntityRecord
{
    public Guid Id { get; set; }
    public string EntityTypeCode { get; set; } = "";   // "estimate"
    public string DataJson { get; set; } = "{}";       // Все поля, кроме системных, в JSON
    public DateTime Created { get; set; } = DateTime.UtcNow;
    public DateTime Modified { get; set; } = DateTime.UtcNow;
}