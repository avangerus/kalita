// Kalita.Application/Services/EntityItemService.cs

using System;
using System.Collections.Generic;
using System.Linq;
using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;
using System.Text.Json;
using Kalita.Application.Services;

public class EntityItemService
{
    private readonly AppDbContext _db;
    private readonly ValidationService _validationService;

    public EntityItemService(AppDbContext db, ValidationService validationService)
    {
        _db = db;
        _validationService = validationService;
    }

    public List<Dictionary<string, object?>> GetAll(string entityTypeCode)
    {
        var entityType = _db.EntityTypes.FirstOrDefault(t => t.Code == entityTypeCode);
        if (entityType == null)
            throw new Exception($"Unknown entity type code: {entityTypeCode}");

        var entities = _db.EntityItems.Where(e => e.TypeCode == entityTypeCode).ToList();
        return entities
            .Select(e => JsonSerializer.Deserialize<Dictionary<string, object?>>(e.DataJson)!)
            .ToList();
    }

    public Dictionary<string, object?>? Get(string entityTypeCode, Guid id)
    {
        var entityType = _db.EntityTypes.FirstOrDefault(t => t.Code == entityTypeCode);
        if (entityType == null)
            throw new Exception($"Unknown entity type code: {entityTypeCode}");

        var entity = _db.EntityItems.FirstOrDefault(e => e.TypeCode == entityTypeCode && e.Id == id);
        return entity == null
            ? null
            : JsonSerializer.Deserialize<Dictionary<string, object?>>(entity.DataJson)!;
    }

    public EntityItem? Create(string entityTypeCode, Dictionary<string, object?> data)
    {
        var entityType = _db.EntityTypes.FirstOrDefault(t => t.Code == entityTypeCode);
        if (entityType == null)
            throw new Exception($"Unknown entity type code: {entityTypeCode}");

        var fields = _db.EntityFields.Where(f => f.EntityTypeId == entityType.Id).ToList();

        var meta = new EntityTypeDto
        {
            Code = entityType.Code,
            DisplayName = entityType.DisplayName,
            Fields = fields.Select(f => new EntityFieldDto
            {
                Code = f.Code,
                DisplayName = f.DisplayName,
                Type = f.FieldType,
                Required = f.IsRequired,
                // Добавь остальные поля, если они есть в EntityFieldDto
            }).ToList()
        };

        var entity = new EntityItem
        {
            Id = Guid.NewGuid(),
            TypeCode = entityTypeCode,
            DataJson = JsonSerializer.Serialize(data)
        };

        // Предполагается, что у тебя есть ValidationService _validationService
        var (valid, error) = _validationService.Validate(entity, meta);
        if (!valid)
            throw new Exception("Validation failed: " + error);

        _db.EntityItems.Add(entity);
        _db.SaveChanges();

        return entity;
    }

    public bool Update(string entityTypeCode, Guid id, Dictionary<string, object?> data)
    {
        var entityType = _db.EntityTypes.FirstOrDefault(t => t.Code == entityTypeCode);
        if (entityType == null)
            throw new Exception($"Unknown entity type code: {entityTypeCode}");

        var entity = _db.EntityItems.FirstOrDefault(e => e.TypeCode == entityTypeCode && e.Id == id);
        if (entity == null) return false;

        var fields = _db.EntityFields.Where(f => f.EntityTypeId == entityType.Id).ToList();


        entity.DataJson = JsonSerializer.Serialize(data);
        _db.SaveChanges();
        return true;
    }

    public bool Delete(string entityTypeCode, Guid id)
    {
        var entityType = _db.EntityTypes.FirstOrDefault(t => t.Code == entityTypeCode);
        if (entityType == null)
            throw new Exception($"Unknown entity type code: {entityTypeCode}");

        var entity = _db.EntityItems.FirstOrDefault(e => e.TypeCode == entityTypeCode && e.Id == id);
        if (entity == null) return false;

        _db.EntityItems.Remove(entity);
        _db.SaveChanges();
        return true;
    }


}
