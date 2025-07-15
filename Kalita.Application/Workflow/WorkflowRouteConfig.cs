using Kalita.Application.Workflow;

namespace Kalita.Application.Workflow
{
    public class WorkflowRouteConfig
    {
        public string Entity { get; set; } // Например, "Estimate"
        public List<WorkflowStep> Steps { get; set; } = new();
        public List<WorkflowTransition> Transitions { get; set; } = new();
        // Можно добавить другие параметры, если нужны
    }
}