namespace Kalita.Domain.Entities;
public class WorkflowStepHistory
{
    public Guid Id { get; set; }
    public Guid EntityId { get; set; }
    public string EntityType { get; set; } = string.Empty;
    public string StepName { get; set; } = string.Empty;
    public string Status { get; set; } = string.Empty;
    public Guid UserId { get; set; }
    public string UserFio { get; set; } = string.Empty;
    public DateTime DateTime { get; set; }
    public string Action { get; set; } = string.Empty;
    public string Comment { get; set; } = string.Empty;
    public string Result { get; set; } = string.Empty;
}