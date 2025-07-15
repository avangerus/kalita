namespace Kalita.Application.Workflow;

public class WorkflowStepConfig
{
    public string Name { get; set; } = string.Empty;
    public WorkflowStepType Type { get; set; }
    public string Status { get; set; } = string.Empty;
    public List<string> Actors { get; set; } = new();
    public string? Condition { get; set; }
    public string? OnEnter { get; set; }
    public string? OnApprove { get; set; }
    public string? OnReject { get; set; }
    public string? EventKey { get; set; }
    public string? ApproveMode { get; set; }
    public List<string>? FieldsRequired { get; set; }
    public List<WorkflowSubStep>? SubSteps { get; set; }
}


public class WorkflowSubStep
{
    public string Name { get; set; } = "";     // Например, "Руководитель отдела"
    public string Actor { get; set; } = "";    // Например, "role:ProjectDirector"
}