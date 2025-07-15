using System;

namespace Kalita.Domain.Entities
{
public class EntityType
{
    public Guid Id { get; set; }
    public string Code { get; set; }           // "project", "brief", "invoice" — уникальный и человекочитаемый
    public string DisplayName { get; set; }
    public string Description { get; set; }
}
}
