namespace Kalita.Domain.Entities;
public class WorkflowStepHistory
{
    public Guid Id { get; set; }
    public Guid EntityId { get; set; }
    public string EntityType { get; set; } = string.Empty;

    public string StepName { get; set; }
    public string Status { get; set; }
    public Guid? UserId { get; set; }
    public string UserFio { get; set; }
    public DateTime DateTime { get; set; }
    public string Action { get; set; }
    public string Comment { get; set; }
    public string Result { get; set; }
}