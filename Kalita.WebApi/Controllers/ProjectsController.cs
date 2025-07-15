using Microsoft.AspNetCore.Mvc;
using Kalita.Application.Services;
using Kalita.WebApi.DTO;
using Kalita.Domain.Entities;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/[controller]")]
public class ProjectsController : KalitaBaseController
{
    private readonly WorkflowEntityService _service;

    public ProjectsController(WorkflowEntityService service)
    {
        _service = service;
    }

    [HttpGet]
    public ActionResult<List<Project>> GetAll()
    {
        var projects = _service.GetAll("Project").Cast<Project>().ToList();
        return Ok(projects);
    }

    [HttpGet("{id}")]
    public ActionResult<Project?> Get(Guid id)
    {
        var project = _service.Get("Project", id) as Project;
        if (project == null) return NotFound();
        return Ok(project);
    }

    [HttpPost]
    public IActionResult Create([FromBody] CreateProjectRequest request)
    {
        var result = _service.CreateProject(request.Name, request.Description, UserId!);
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Project);
    }

    [HttpPut("{id}")]
    public IActionResult Update(Guid id, [FromBody] UpdateProjectRequest request)
    {
        var result = _service.UpdateProject(id, request.Name, request.Description);
        if (!result.Success) return BadRequest(result.Error);
        return Ok(result.Project);
    }

    [HttpDelete("{id}")]
    public IActionResult Delete(Guid id)
    {
        var result = _service.DeleteProject(id);
        if (!result.Success) return BadRequest(result.Error);
        return Ok();
    }
}
