
namespace Kalita.Application.Workflow;

public class WorkflowRouteConfig
{
    public string Entity { get; set; } = string.Empty;
    public List<string> Statuses { get; set; } = new();
    public List<WorkflowStepConfig> Steps { get; set; } = new();
    public List<WorkflowTransitionConfig> Transitions { get; set; } = new();
}
