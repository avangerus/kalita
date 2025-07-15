namespace Kalita.Application.DTO
{
    public class WorkflowRouteStepDto
    {
        public string Step { get; set; } = "";
        public string Status { get; set; } = "";
        public List<string> Actors { get; set; } = new();
        public bool Current { get; set; }
    }

    public class WorkflowRouteHistoryDto
    {
        public string Step { get; set; } = "";
        public string? User { get; set; }
        public string? Role { get; set; }
        public DateTime? DateTime { get; set; }
        public string? Action { get; set; }
        public string? Comment { get; set; }
    }

    public class WorkflowRouteInfoDto
    {
        public List<WorkflowRouteStepDto> Route { get; set; } = new();
        public List<WorkflowRouteHistoryDto> History { get; set; } = new();
        public string? CurrentStep { get; set; }
        public bool CanApprove { get; set; }
        public bool CanReject { get; set; }
        public bool CanReturn { get; set; }
    }
}
