using Kalita.Application.Workflow;


namespace Kalita.Application.Workflow
{
    public class WorkflowStep
    {
        public string Status { get; set; }
        public string Name { get; set; }
        public List<string> Actors { get; set; } = new();
        public List<string>? FieldsRequired { get; set; }
        // Добавь другие нужные поля, если есть
    }
}
