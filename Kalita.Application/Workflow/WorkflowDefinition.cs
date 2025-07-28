using Kalita.Application.Workflow;

public class WorkflowDefinition
{
    public string Type { get; set; }
    public string DisplayName { get; set; }
    public string InitialStatus { get; set; }
    public List<WorkflowStatus> Statuses { get; set; }
    public List<WorkflowAction> GlobalActions { get; set; }
}

public class WorkflowStatus
{
    public string Code { get; set; }
    public string DisplayName { get; set; }
    public string Stage { get; set; }
    public List<WorkflowAction> Actions { get; set; }
    public List<string> EditRights { get; set; }
    public List<string> ViewRights { get; set; }
    public string UiMask { get; set; }
    public List<string> Modifiers { get; set; }
    public List<WorkflowCondition> Conditions { get; set; }
}


public class WorkflowAction
{
    public string Code { get; set; }
    public string DisplayName { get; set; }
    public List<string> Roles { get; set; }
    public string ToStatus { get; set; }
    public bool RequiresComment { get; set; }
    public bool AllMustApprove { get; set; }
    public List<string> FromStatuses { get; set; }
}

public class WorkflowCondition
{
    public string If { get; set; }
    public string Then { get; set; }
}
// public class WorkflowState
// {
//     public string Code { get; set; }
//     public string Name { get; set; }
//     public List<WorkflowAction>? OnEnter { get; set; }
// }

// public class WorkflowTransition
// {
//     public string From { get; set; }
//     public string To { get; set; }
//     public string Action { get; set; }
//     public string Name { get; set; }
//     public List<string>? Roles { get; set; }
//     public List<WorkflowCondition>? Conditions { get; set; }
//     public bool CommentRequired { get; set; }
// }



