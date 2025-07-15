namespace Kalita.WebApi.DTO
{
    public class ApproveParallelRequest
    {
        public string SubStepName { get; set; } = "";
        public string UserRole { get; set; } = "";
        public string? Comment { get; set; }
    }
}