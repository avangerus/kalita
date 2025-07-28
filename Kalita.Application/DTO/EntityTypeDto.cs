
using Kalita.Application.DTO;

public class EntityTypeDto
{
    public string Code { get; set; }
    public string DisplayName { get; set; }
    public string? Description { get; set; }
    public List<EntityFieldDto> Fields { get; set; }
}