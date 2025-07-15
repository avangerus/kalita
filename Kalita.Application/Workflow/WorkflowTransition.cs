namespace Kalita.Application.Workflow
{
    public class WorkflowTransition
    {
        public string From { get; set; } = string.Empty;
        public string To { get; set; } = string.Empty;
        public string? Action { get; set; }         // Новое поле
        public List<string>? Roles { get; set; }    // Новое поле (может быть null)
        public List<WorkflowCondition>? Conditions { get; set; } // Новое поле
        public bool? CommentRequired { get; set; }  // Новое поле

        public string? Condition { get; set; }      // Оставь, если используешь старую логику
        // Добавляй другие поля при необходимости
    }
}