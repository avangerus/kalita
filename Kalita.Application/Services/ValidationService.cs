// Kalita.Application/Services/ValidationService.cs

using Kalita.Domain.Entities;
using Kalita.Application.DTO; // Используй правильный namespace для EntityTypeDto и EntityFieldDto
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Kalita.Application.Services
{
    /// <summary>
    /// Валидатор динамических сущностей по их метаданным.
    /// </summary>
    public class ValidationService
    {
        /// <summary>
        /// Валидирует объект EntityItem на основе метаданных (EntityTypeDto).
        /// </summary>
        /// <param name="item">Сохраняемая/изменяемая сущность</param>
        /// <param name="meta">Мета-схема типа</param>
        /// <returns>(успех, сообщение об ошибке)</returns>
        public (bool Valid, string Error) Validate(EntityItem item, EntityTypeDto meta)
        {
            if (item == null)
                return (false, "EntityItem is null");
            if (meta == null)
                return (false, "EntityType metadata not found");

            Dictionary<string, object?>? data = null;
            try
            {
                data = JsonSerializer.Deserialize<Dictionary<string, object?>>(item.DataJson ?? "{}");
            }
            catch
            {
                return (false, "Invalid JSON in entity data");
            }
            if (data == null)
                return (false, "Empty or invalid entity data");

            foreach (var field in meta.Fields)
            {
                // 1. Required
                if (field.Required)
                {
                    if (!data.ContainsKey(field.Code) || data[field.Code] == null ||
                        (data[field.Code] is string s && string.IsNullOrWhiteSpace(s)))
                        return (false, $"Поле \"{field.DisplayName ?? field.Code}\" обязательно для заполнения");
                }

                // Если не заполнено — дальше не валидируем
                if (!data.ContainsKey(field.Code) || data[field.Code] == null)
                    continue;

                var value = data[field.Code];

                // 2. Multi/Collection
                if (field.IsCollection == true || field.Type == "collection")
                {
                    if (value is JsonElement je && je.ValueKind == JsonValueKind.Array)
                        continue;
                    if (value is IEnumerable<object>)
                        continue;
                    return (false, $"Поле \"{field.DisplayName}\" должно быть массивом (списком)");
                }

                // 3. Enum (AllowedValues)
                if (field.Type == "enum" || (field.AllowedValues != null && field.AllowedValues.Any())
                    || (field.EnumOptions != null && field.EnumOptions.Any())
                    || (field.Values != null && field.Values.Any()))
                {
                    var allowed = field.AllowedValues
                        ?? field.EnumOptions
                        ?? field.Values
                        ?? new List<string>();
                    var str = value?.ToString() ?? "";
                    if (allowed.Any() && !allowed.Contains(str))
                        return (false, $"Поле \"{field.DisplayName}\" содержит недопустимое значение");
                }

                // 4. Lookup
                if (field.Type == "lookup" || !string.IsNullOrEmpty(field.ReferenceTypeCode))
                {
                    if (string.IsNullOrWhiteSpace(value?.ToString()))
                        return (false, $"Поле \"{field.DisplayName}\" должно содержать ссылку на другую сущность ({field.ReferenceTypeCode})");
                    // Для MVP — просто не пустое значение, можно потом сделать проверку существования по БД
                }

                // 5. Типы
                switch (field.Type)
                {
                    case "string":
                    case "text":
                        if (!(value is string) && !(value is JsonElement je1 && je1.ValueKind == JsonValueKind.String))
                            return (false, $"Поле \"{field.DisplayName}\" должно быть строкой");
                        break;
                    case "number":
                    case "decimal":
                        if (value is JsonElement jeNum && jeNum.ValueKind == JsonValueKind.Number)
                            break;
                        if (!decimal.TryParse(value?.ToString(), out _))
                            return (false, $"Поле \"{field.DisplayName}\" должно быть числом");
                        break;
                    case "bool":
                    case "boolean":
                        if (value is JsonElement jeBool)
                        {
                            if (jeBool.ValueKind == JsonValueKind.True || jeBool.ValueKind == JsonValueKind.False)
                                break;
                        }
                        if (!bool.TryParse(value?.ToString(), out _))
                            return (false, $"Поле \"{field.DisplayName}\" должно быть булевым значением");
                        break;
                    case "date":
                        if (value is JsonElement jeDate && jeDate.ValueKind == JsonValueKind.String && DateTime.TryParse(jeDate.GetString(), out _))
                            break;
                        if (!DateTime.TryParse(value?.ToString(), out _))
                            return (false, $"Поле \"{field.DisplayName}\" должно быть датой");
                        break;
                }
            }

            return (true, "");
        }

    }
}
