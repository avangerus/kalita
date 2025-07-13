namespace Kalita.Application.Workflow;

public class WorkflowTransitionConfig
{
    public string From { get; set; } = string.Empty;
    public string To { get; set; } = string.Empty;
    public string? Condition { get; set; }
}
