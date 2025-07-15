using System;

namespace Kalita.Domain.Entities
{
public class EntityItem
{
    public Guid Id { get; set; }
    // public Guid EntityTypeId { get; set; }     // В БД связь через Id
    public string TypeCode { get; set; } = null!;
    public string DataJson { get; set; }
    public string Status { get; set; }
    public DateTime CreatedAt { get; set; }
    public DateTime UpdatedAt { get; set; }
    public string CreatedBy { get; set; }
    public string UpdatedBy { get; set; }
}
}
