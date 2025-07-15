// Kalita.Application/Services/DynamicEntityService.cs

using System;
using System.Collections.Generic;
using System.Linq;
using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;
using System.Text.Json;

public class DynamicEntityService
{
    private readonly AppDbContext _db;
    private readonly EntityMetadataService _entityMetadataService;

    public DynamicEntityService(AppDbContext db, EntityMetadataService entityMetadataService)
    {
        _db = db;
        _entityMetadataService = entityMetadataService;
    }

    public List<Dictionary<string, object?>> GetAll(string entityTypeCode)
    {
        var entities = _db.DynamicEntities.Where(e => e.TypeCode == entityTypeCode).ToList();
        return entities
            .Select(e => JsonSerializer.Deserialize<Dictionary<string, object?>>(e.JsonData)!)
            .ToList();
    }

    public Dictionary<string, object?>? Get(string entityTypeCode, Guid id)
    {
        var entity = _db.DynamicEntities.FirstOrDefault(e => e.TypeCode == entityTypeCode && e.Id == id);
        return entity == null
            ? null
            : JsonSerializer.Deserialize<Dictionary<string, object?>>(entity.JsonData)!;
    }

    public DynamicEntity? Create(string entityTypeCode, Dictionary<string, object?> data)
    {
        // 1. Валидация данных по метаданным
        var (ok, error) = ValidateData(entityTypeCode, data);
        if (!ok)
            throw new Exception(error);

        // 2. Получаем метаданные типа сущности
        var typeMeta = _entityMetadataService.GetTypeByCode(entityTypeCode);
        if (typeMeta == null)
            throw new Exception($"Unknown entity type: {entityTypeCode}");

        // 3. Генерируем Id, сериализуем данные
        var entity = new DynamicEntity
        {
            Id = Guid.NewGuid(),
            TypeCode = entityTypeCode,
            JsonData = JsonSerializer.Serialize(data)
        };

        _db.DynamicEntities.Add(entity);
        _db.SaveChanges();

        return entity;
    }

    public bool Update(string entityTypeCode, Guid id, Dictionary<string, object?> data)
    {
        var entity = _db.DynamicEntities.FirstOrDefault(e => e.TypeCode == entityTypeCode && e.Id == id);
        if (entity == null) return false;

        entity.JsonData = JsonSerializer.Serialize(data);
        _db.SaveChanges();
        return true;
    }

    public bool Delete(string entityTypeCode, Guid id)
    {
        var entity = _db.DynamicEntities.FirstOrDefault(e => e.TypeCode == entityTypeCode && e.Id == id);
        if (entity == null) return false;

        _db.DynamicEntities.Remove(entity);
        _db.SaveChanges();
        return true;
    }

    public (bool Success, string? Error) ValidateData(string entityTypeCode, Dictionary<string, object?> data)
    {
        var meta = _entityMetadataService.GetTypeByCode(entityTypeCode);
        if (meta is null)
            return (false, "Entity type not found");

        foreach (var field in meta.Fields)
        {
            if (field.Required && (!data.ContainsKey(field.Name) || data[field.Name] == null))
                return (false, $"Field {field.Name} is required.");

            if (data.TryGetValue(field.Name, out var val) && val != null)
            {
                if (field.Type == "decimal" && !(val is decimal || decimal.TryParse(val.ToString(), out _)))
                    return (false, $"Field {field.Name} must be decimal.");
                if (field.Type == "int" && !(val is int || int.TryParse(val.ToString(), out _)))
                    return (false, $"Field {field.Name} must be int.");
                if (field.Type == "string" && !(val is string))
                    return (false, $"Field {field.Name} must be string.");
                // Допиши reference, date и др. по необходимости!
            }
        }
        return (true, null);
    }
}
