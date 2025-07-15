using Microsoft.AspNetCore.Mvc;
using Kalita.Application.Services;
using Kalita.Domain.Entities;
using Kalita.WebApi.DTO;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/dictionaries")]
public class DictionariesController : KalitaBaseController
{
    private readonly DictionaryService _dictionaryService;

    // public DictionariesController(DictionaryService dictionaryService)
    // {
    //     _dictionaryService = dictionaryService;
    // }
    private readonly DictionaryMetadataService _metaService;
    public DictionariesController(DictionaryService service, DictionaryMetadataService metaService)
    {
        _dictionaryService = service;
        _metaService = metaService;
    }

    [HttpGet("types/meta")]
    public IActionResult GetTypesMeta()
    {
        var meta = _metaService.GetTypes();
        return Ok(meta);
    }

    // --- Типы справочников ---

    [HttpGet("types")]
    public IActionResult GetTypes()
    {
        var types = _dictionaryService.GetTypes();
        return Ok(types);
    }

    [HttpPost("types")]
    public IActionResult CreateType([FromBody] CreateDictionaryTypeRequest req)
    {
        var result = _dictionaryService.CreateType(req.Code, req.Name, req.Description);
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Type);
    }

    // --- Элементы справочников ---

    [HttpGet("{typeCode}/items")]
    public IActionResult GetItems(string typeCode)
    {
        var items = _dictionaryService.GetItemsByTypeCode(typeCode);
        return Ok(items);
    }



    [HttpPost("{typeCode}/items")]
    public IActionResult CreateItem(string typeCode, [FromBody] CreateDictionaryItemRequest req)
    {
        var types = _dictionaryService.GetTypes();
        var type = types.FirstOrDefault(t => t.Code == typeCode);
        if (type == null) return BadRequest("Dictionary type not found");

        var result = _dictionaryService.CreateItem(type.Id, req.Value, req.Code, req.ExtraJson, req.ParentId);
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Item);
    }


    [HttpPut("items/{id}")]
    public IActionResult UpdateItem(Guid id, [FromBody] UpdateDictionaryItemRequest req)
    {
        var result = _dictionaryService.UpdateItem(id, req.Value, req.Code, req.ExtraJson);
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Item);
    }

    [HttpDelete("items/{id}")]
    public IActionResult DeleteItem(Guid id)
    {
        if (_dictionaryService.DeleteItem(id))
            return Ok();
        return NotFound();
    }
}
