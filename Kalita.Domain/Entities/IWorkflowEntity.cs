namespace Kalita.Domain.Entities
{
    public interface IWorkflowEntity
    {
        Guid Id { get; }
        string Status { get; set; }
    }
}