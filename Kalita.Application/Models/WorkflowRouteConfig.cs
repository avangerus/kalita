namespace Kalita.Application.Workflow;

public class WorkflowRouteConfig
{
    public string Entity { get; set; } = "";
    public List<string> Statuses { get; set; } = new();
    public List<WorkflowStep> Steps { get; set; } = new();
    public List<WorkflowTransition> Transitions { get; set; } = new();
}

public class WorkflowStep
{
    public string Name { get; set; } = "";
    public string Type { get; set; } = "";
    public string Status { get; set; } = "";
    public List<string> Actors { get; set; } = new();
    public List<string>? FieldsRequired { get; set; }
    public List<WorkflowSubStep>? SubSteps { get; set; }
}

public class WorkflowTransition
{
    public string From { get; set; } = "";
    public string To { get; set; } = "";
    public string? Condition { get; set; }
}
