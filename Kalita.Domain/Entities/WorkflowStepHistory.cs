namespace Kalita.Domain.Entities;

public class WorkflowStepHistory
{
    public Guid Id { get; set; }
    public Guid EntityId { get; set; }
    public string EntityType { get; set; } = "";
    public string StepName { get; set; } = "";
    public string? SubStepName { get; set; } // <--- добавляем!
    public string Status { get; set; } = "";
    public string UserId { get; set; } = "";
    public string UserFio { get; set; } = "";
    public DateTime DateTime { get; set; }
    public string Action { get; set; } = "";
    public string? Comment { get; set; }
    public string Result { get; set; } = "";
    public string? UserRole { get; set; }      // Чтобы хранить роль (для проверки IsAllParallelApproved)

}