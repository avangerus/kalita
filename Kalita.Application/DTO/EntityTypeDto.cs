
using Kalita.Application.DTO;

public class EntityTypeDto
{
    public string Code { get; set; }
    public string Name { get; set; }
    public string? Description { get; set; }
    public List<EntityFieldDto> Fields { get; set; }
}