namespace Kalita.Application.Workflow;

public enum WorkflowStepType
{
    Approve,
    Decision,
    SystemAction,
    Notification,
    Conditional,
    WaitForEvent,
    Pusher,
    Input,
    Review,
    Parallel,
    Manual,
    Empty
}
