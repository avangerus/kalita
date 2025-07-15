using Microsoft.AspNetCore.Mvc;
using Kalita.Application.Services;
using Kalita.Domain.Entities;

[ApiController]
[Route("api/entities/{entityTypeCode}")]
public class EntityItemController : ControllerBase
{
    private readonly EntityItemService _service;
    public EntityItemController(EntityItemService service) => _service = service;

    [HttpGet]
    public IActionResult GetAll(string entityTypeCode)
    {
        var list = _service.GetAll(entityTypeCode);
        return Ok(list);
    }

    [HttpGet("{id}")]
    public IActionResult Get(string entityTypeCode, Guid id)
    {
        var entity = _service.Get(entityTypeCode, id);
        if (entity == null) return NotFound();
        return Ok(entity);
    }

    [HttpPost]
    public IActionResult Create(string entityTypeCode, [FromBody] Dictionary<string, object?> data)
    {
        try
        {
            var entity = _service.Create(entityTypeCode, data);
            return Ok(entity);
        }
        catch (Exception ex)
        {
            return BadRequest(ex.Message);
        }
    }

    [HttpPut("{id}")]
    public IActionResult Update(string entityTypeCode, Guid id, [FromBody] Dictionary<string, object?> data)
    {
        var ok = _service.Update(entityTypeCode, id, data);
        if (!ok) return NotFound();
        return Ok();
    }

    [HttpDelete("{id}")]
    public IActionResult Delete(string entityTypeCode, Guid id)
    {
        var ok = _service.Delete(entityTypeCode, id);
        if (!ok) return NotFound();
        return Ok();
    }



}
