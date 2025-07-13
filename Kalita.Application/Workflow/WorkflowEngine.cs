using System.Text.Json;
using Kalita.Domain.Entities;

namespace Kalita.Application.Workflow;

public class WorkflowEngine
{
    private readonly WorkflowRouteConfig _config;

    public WorkflowEngine(string jsonConfigPath)
    {
        var json = File.ReadAllText(jsonConfigPath);

        var options = new JsonSerializerOptions
        {
            PropertyNameCaseInsensitive = true
        };
        options.Converters.Add(new System.Text.Json.Serialization.JsonStringEnumConverter());

        _config = JsonSerializer.Deserialize<WorkflowRouteConfig>(json, options)
            ?? throw new Exception("Workflow config not found or invalid.");
    }

    // Для DI или тестов
    public WorkflowEngine(WorkflowRouteConfig config)
    {
        _config = config;
    }

    // Проверить возможность перехода по маршруту
    public bool CanTransition(string currentStatus, string nextStatus, object entity, string userRole = "role:Test")
    {
        var transition = _config.Transitions
            .FirstOrDefault(t => (t.From == currentStatus || t.From == "Any") && t.To == nextStatus);

        if (transition == null) return false;

        // Проверка условий (например, FieldsFilled)
        if (!string.IsNullOrEmpty(transition.Condition))
        {
            if (transition.Condition == "FieldsFilled")
            {
                var step = _config.Steps.FirstOrDefault(s => s.Status == currentStatus);
                if (step?.FieldsRequired != null)
                {
                    foreach (var field in step.FieldsRequired)
                    {
                        var prop = entity.GetType().GetProperty(field);
                        if (prop == null || prop.GetValue(entity) == null)
                            return false;
                    }
                }
            }
        }

        // === Новый блок: Проверка Actors ===
        var stepNext = _config.Steps.FirstOrDefault(s => s.Status == nextStatus);
        if (stepNext != null && stepNext.Actors.Any())
        {
            // userRole должен совпадать с одним из Actors шага!
            if (!stepNext.Actors.Contains(userRole))
                return false;
        }
        // ================================

        return true;
    }



    // Получить текущий шаг
    public WorkflowStepConfig? GetCurrentStep(string status)
    {
        return _config.Steps.FirstOrDefault(s => s.Status == status);
    }

    // Получить доступные переходы из текущего статуса
    public List<string> GetNextStatuses(string currentStatus)
    {
        return _config.Transitions
            .Where(t => t.From == currentStatus || t.From == "Any")
            .Select(t => t.To)
            .Distinct()
            .ToList();
    }

    // Получить шаги для статуса
    public List<WorkflowStepConfig> GetStepsForStatus(string status)
    {
        return _config.Steps.Where(s => s.Status == status).ToList();
    }

    // (по желанию) Получить все доступные действия для пользователя (роль + статус)
    public List<WorkflowStepConfig> GetAvailableSteps(string status, string userRole)
    {
        return _config.Steps
            .Where(s => s.Status == status && s.Actors.Contains(userRole))
            .ToList();
    }

    // Для будущего: проверка прав, сложные условия, вычисление сценариев.
}
