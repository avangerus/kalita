using Kalita.Application.Workflow;

public class WorkflowDefinition
{
    public string InitialState { get; set; }
    public List<WorkflowState> States { get; set; }
    public List<WorkflowTransition> Transitions { get; set; }
}

public class WorkflowState
{
    public string Code { get; set; }
    public string Name { get; set; }
    public List<WorkflowAction>? OnEnter { get; set; }
}

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


    public class WorkflowCondition
    {
        public string Field { get; set; } = string.Empty;
        public string Operator { get; set; } = string.Empty;
        public string Value { get; set; } = string.Empty;
    }


public class WorkflowAction
{
    public string Action { get; set; }
    public string Field { get; set; }
    public object Value { get; set; }
}
